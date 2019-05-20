package api

//go:generate interfacer -for github.com/buildkite/agent/api.Client -as agent.APIClient -o ../agent/api_iface.go

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httputil"
	"net/url"
	"reflect"
	"strings"
	"time"

	"github.com/buildkite/agent/logger"
	"github.com/google/go-querystring/query"
)

const (
	defaultBaseURL   = "https://agent.buildkite.com/"
	defaultUserAgent = "buildkite-agent/api"
)

// ClientConfig is configuration for Client
type ClientConfig struct {
	// Base URL for API requests. Defaults to the public Buildkite Agent API.
	// The URL should always be specified with a trailing slash.
	BaseURL *url.URL

	// User agent used when communicating with the Buildkite Agent API.
	UserAgent string

	// If true, requests and responses will be dumped and set to the logger
	DebugHTTP bool
}

// A Client manages communication with the Buildkite Agent API.
type Client struct {
	// The client configuration
	conf ClientConfig

	// HTTP client used to communicate with the API.
	client *http.Client

	// The logger used
	logger logger.Logger
}

// NewClient returns a new Buildkite Agent API Client.
func NewClient(httpClient *http.Client, l logger.Logger, conf ClientConfig) *Client {
	if conf.BaseURL == nil {
		conf.BaseURL, _ = url.Parse(defaultBaseURL)
	}

	if conf.UserAgent == "" {
		conf.UserAgent = defaultUserAgent
	}

	return &Client{
		logger: l,
		client: httpClient,
		conf:   conf,
	}
}

// NewRequest creates an API request. A relative URL can be provided in urlStr,
// in which case it is resolved relative to the BaseURL of the Client.
// Relative URLs should always be specified without a preceding slash. If
// specified, the value pointed to by body is JSON encoded and included as the
// request body.
func (c *Client) newRequest(method, urlStr string, body interface{}) (*http.Request, error) {
	u := joinURL(c.conf.BaseURL.String(), urlStr)

	buf := new(bytes.Buffer)
	if body != nil {
		err := json.NewEncoder(buf).Encode(body)
		if err != nil {
			return nil, err
		}
	}

	req, err := http.NewRequest(method, u, buf)
	if err != nil {
		return nil, err
	}

	req.Header.Add("User-Agent", c.conf.UserAgent)

	if body != nil {
		req.Header.Add("Content-Type", "application/json")
	}

	return req, nil
}

// NewFormRequest creates an multi-part form request. A relative URL can be
// provided in urlStr, in which case it is resolved relative to the UploadURL
// of the Client. Relative URLs should always be specified without a preceding
// slash.
func (c *Client) newFormRequest(method, urlStr string, body *bytes.Buffer) (*http.Request, error) {
	u := joinURL(c.conf.BaseURL.String(), urlStr)

	req, err := http.NewRequest(method, u, body)
	if err != nil {
		return nil, err
	}

	if c.conf.UserAgent != "" {
		req.Header.Add("User-Agent", c.conf.UserAgent)
	}

	return req, nil
}

// Response is a Buildkite Agent API response. This wraps the standard
// http.Response.
type Response struct {
	*http.Response
}

// newResponse creates a new Response for the provided http.Response.
func newResponse(r *http.Response) *Response {
	response := &Response{Response: r}
	return response
}

// Do sends an API request and returns the API response. The API response is
// JSON decoded and stored in the value pointed to by v, or returned as an
// error if an API error has occurred.  If v implements the io.Writer
// interface, the raw response body will be written to v, without attempting to
// first decode it.
func (c *Client) doRequest(req *http.Request, v interface{}) (*Response, error) {
	var err error

	if c.conf.DebugHTTP {
		// If the request is a multi-part form, then it's probably a
		// file upload, in which case we don't want to spewing out the
		// file contents into the debug log (especially if it's been
		// gzipped)
		var requestDump []byte
		if strings.Contains(req.Header.Get("Content-Type"), "multipart/form-data") {
			requestDump, err = httputil.DumpRequestOut(req, false)
		} else {
			requestDump, err = httputil.DumpRequestOut(req, true)
		}

		c.logger.Debug("ERR: %s\n%s", err, string(requestDump))
	}

	ts := time.Now()

	c.logger.Debug("%s %s", req.Method, req.URL)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}

	c.logger.WithFields(
		logger.StringField(`proto`, resp.Proto),
		logger.IntField(`status`, resp.StatusCode),
		logger.DurationField(`Δ`, time.Since(ts)),
	).Debug("↳ %s %s", req.Method, req.URL)

	defer resp.Body.Close()
	defer io.Copy(ioutil.Discard, resp.Body)

	response := newResponse(resp)

	if c.conf.DebugHTTP {
		responseDump, err := httputil.DumpResponse(resp, true)
		c.logger.Debug("\nERR: %s\n%s", err, string(responseDump))
	}

	err = checkResponse(resp)
	if err != nil {
		// even though there was an error, we still return the response
		// in case the caller wants to inspect it further
		return response, err
	}

	if v != nil {
		if w, ok := v.(io.Writer); ok {
			io.Copy(w, resp.Body)
		} else {
			if strings.Contains(req.Header.Get("Content-Type"), "application/msgpack") {
				err = errors.New("Msgpack not supported")
			} else {
				err = json.NewDecoder(resp.Body).Decode(v)
			}
		}
	}

	return response, err
}

// ErrorResponse provides a message.
type ErrorResponse struct {
	Response *http.Response // HTTP response that caused this error
	Message  string         `json:"message"` // error message
}

func (r *ErrorResponse) Error() string {
	s := fmt.Sprintf("%v %v: %d",
		r.Response.Request.Method, r.Response.Request.URL,
		r.Response.StatusCode)

	if r.Message != "" {
		s = fmt.Sprintf("%s %v", s, r.Message)
	}

	return s
}

func checkResponse(r *http.Response) error {
	if c := r.StatusCode; 200 <= c && c <= 299 {
		return nil
	}

	errorResponse := &ErrorResponse{Response: r}
	data, err := ioutil.ReadAll(r.Body)
	if err == nil && data != nil {
		json.Unmarshal(data, errorResponse)
	}

	return errorResponse
}

// addOptions adds the parameters in opt as URL query parameters to s. opt must
// be a struct whose fields may contain "url" tags.
func addOptions(s string, opt interface{}) (string, error) {
	v := reflect.ValueOf(opt)
	if v.Kind() == reflect.Ptr && v.IsNil() {
		return s, nil
	}

	u, err := url.Parse(s)
	if err != nil {
		return s, err
	}

	qs, err := query.Values(opt)
	if err != nil {
		return s, err
	}

	u.RawQuery = qs.Encode()
	return u.String(), nil
}

func joinURL(endpoint string, path string) string {
	return strings.TrimRight(endpoint, "/") + "/" + strings.TrimLeft(path, "/")
}
