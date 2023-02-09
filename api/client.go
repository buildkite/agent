package api

//go:generate interfacer -for github.com/buildkite/agent/v3/api.Client -as agent.APIClient -o ../agent/api.go

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"reflect"
	"strings"
	"time"

	"github.com/buildkite/agent/v3/logger"
	"github.com/google/go-querystring/query"
)

const (
	defaultEndpoint  = "https://agent.buildkite.com/"
	defaultUserAgent = "buildkite-agent/api"
)

// Config is configuration for the API Client
type Config struct {
	// Endpoint for API requests. Defaults to the public Buildkite Agent API.
	// The URL should always be specified with a trailing slash.
	Endpoint string

	// The authentication token to use, either a registration or access token
	Token string

	// User agent used when communicating with the Buildkite Agent API.
	UserAgent string

	// If true, only HTTP2 is disabled
	DisableHTTP2 bool

	// If true, requests and responses will be dumped and set to the logger
	DebugHTTP bool

	// The http client used, leave nil for the default
	HTTPClient *http.Client
}

// A Client manages communication with the Buildkite Agent API.
type Client struct {
	// The client configuration
	conf Config

	// HTTP client used to communicate with the API.
	client *http.Client

	// The logger used
	logger logger.Logger
}

// NewClient returns a new Buildkite Agent API Client.
func NewClient(l logger.Logger, conf Config) *Client {
	if conf.Endpoint == "" {
		conf.Endpoint = defaultEndpoint
	}

	if conf.UserAgent == "" {
		conf.UserAgent = defaultUserAgent
	}

	httpClient := conf.HTTPClient
	if conf.HTTPClient == nil {
		t := &http.Transport{
			Proxy:              http.ProxyFromEnvironment,
			DisableCompression: false,
			DisableKeepAlives:  false,
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			MaxIdleConns:        100,
			IdleConnTimeout:     90 * time.Second,
			TLSHandshakeTimeout: 30 * time.Second,
		}

		if conf.DisableHTTP2 {
			t.TLSNextProto = make(map[string]func(string, *tls.Conn) http.RoundTripper)
		}

		httpClient = &http.Client{
			Timeout: 60 * time.Second,
			Transport: &authenticatedTransport{
				Token:    conf.Token,
				Delegate: t,
			},
		}
	}

	return &Client{
		logger: l,
		client: httpClient,
		conf:   conf,
	}
}

// Config returns the internal configuration for the Client
func (c *Client) Config() Config {
	return c.conf
}

// FromAgentRegisterResponse returns a new instance using the access token and endpoint
// from the registration response
func (c *Client) FromAgentRegisterResponse(resp *AgentRegisterResponse) *Client {
	conf := c.conf

	// Override the registration token with the access token
	conf.Token = resp.AccessToken

	// If Buildkite told us to use a new Endpoint, respect that
	if resp.Endpoint != "" {
		conf.Endpoint = resp.Endpoint
	}

	return NewClient(c.logger, conf)
}

// FromPing returns a new instance using a new endpoint from a ping response
func (c *Client) FromPing(resp *Ping) *Client {
	conf := c.conf

	// If Buildkite told us to use a new Endpoint, respect that
	if resp.Endpoint != "" {
		conf.Endpoint = resp.Endpoint
	}

	return NewClient(c.logger, conf)
}

type Header struct {
	Name  string
	Value string
}

// NewRequest creates an API request. A relative URL can be provided in urlStr,
// in which case it is resolved relative to the BaseURL of the Client.
// Relative URLs should always be specified without a preceding slash. If
// specified, the value pointed to by body is JSON encoded and included as the
// request body.
func (c *Client) newRequest(
	ctx context.Context,
	method, urlStr string,
	body any,
	headers ...Header,
) (*http.Request, error) {
	u := joinURLPath(c.conf.Endpoint, urlStr)

	buf := new(bytes.Buffer)
	if body != nil {
		err := json.NewEncoder(buf).Encode(body)
		if err != nil {
			return nil, err
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, u, buf)
	if err != nil {
		return nil, err
	}

	req.Header.Add("User-Agent", c.conf.UserAgent)

	for _, header := range headers {
		req.Header.Add(header.Name, header.Value)
	}

	if body != nil {
		req.Header.Add("Content-Type", "application/json")
	}

	return req, nil
}

// NewFormRequest creates an multi-part form request. A relative URL can be
// provided in urlStr, in which case it is resolved relative to the UploadURL
// of the Client. Relative URLs should always be specified without a preceding
// slash.
func (c *Client) newFormRequest(ctx context.Context, method, urlStr string, body *bytes.Buffer) (*http.Request, error) {
	u := joinURLPath(c.conf.Endpoint, urlStr)

	req, err := http.NewRequestWithContext(ctx, method, u, body)
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
func (c *Client) doRequest(req *http.Request, v any) (*Response, error) {
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

		if err != nil {
			c.logger.Debug("ERR: %s\n%s", err, string(requestDump))
		} else {
			c.logger.Debug("%s", string(requestDump))
		}
	}

	ts := time.Now()

	c.logger.Debug("%s %s", req.Method, req.URL)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}

	c.logger.WithFields(
		logger.StringField("proto", resp.Proto),
		logger.IntField("status", resp.StatusCode),
		logger.DurationField("Δ", time.Since(ts)),
	).Debug("↳ %s %s", req.Method, req.URL)

	defer resp.Body.Close()
	defer io.Copy(io.Discard, resp.Body)

	response := newResponse(resp)

	if c.conf.DebugHTTP {
		responseDump, err := httputil.DumpResponse(resp, true)
		if err != nil {
			c.logger.Debug("\nERR: %s\n%s", err, string(responseDump))
		} else {
			c.logger.Debug("\n%s", string(responseDump))
		}
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

func IsErrHavingStatus(err error, code int) bool {
	var apierr *ErrorResponse
	return errors.As(err, &apierr) && apierr.Response.StatusCode == code
}

func checkResponse(r *http.Response) error {
	if c := r.StatusCode; 200 <= c && c <= 299 {
		return nil
	}

	errorResponse := &ErrorResponse{Response: r}
	data, err := io.ReadAll(r.Body)
	if err == nil && data != nil {
		json.Unmarshal(data, errorResponse)
	}

	return errorResponse
}

// addOptions adds the parameters in opt as URL query parameters to s. opt must
// be a struct whose fields may contain "url" tags.
func addOptions(s string, opt any) (string, error) {
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

func joinURLPath(endpoint string, path string) string {
	return strings.TrimRight(endpoint, "/") + "/" + strings.TrimLeft(path, "/")
}
