package api

//go:generate go run github.com/rjeczalik/interfaces/cmd/interfacer@v0.3.0 -for github.com/buildkite/agent/v3/api.Client -as agent.APIClient -o ../agent/api.go

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/buildkite/agent/v3/internal/agenthttp"
	"github.com/buildkite/agent/v3/logger"
	"github.com/google/go-querystring/query"
)

const (
	defaultEndpoint  = "https://agent.buildkite.com/v3"
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

	// http client profile to use for the client
	HTTPClientProfile string

	// If true, requests and responses will be dumped and set to the logger
	DebugHTTP bool

	// If true timings for each request will be logged
	TraceHTTP bool

	// The http client used, leave nil for the default
	HTTPClient *http.Client

	// optional TLS configuration primarily used for testing
	TLSConfig *tls.Config
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

	if conf.HTTPClient != nil {
		return &Client{
			logger: l,
			client: conf.HTTPClient,
			conf:   conf,
		}
	}

	return &Client{
		logger: l,
		client: agenthttp.NewClient(
			agenthttp.WithAuthToken(conf.Token),
			agenthttp.WithAllowHTTP2(!conf.DisableHTTP2),
			agenthttp.WithTLSConfig(conf.TLSConfig),
			agenthttp.WithHTTPClientProfile(conf.HTTPClientProfile),
		),
		conf: conf,
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

	// If our context has a timeout/deadline, tell the server how long is remaining.
	// This may allow the server to configure its own timeouts accordingly.
	if deadline, ok := ctx.Deadline(); ok {
		ms := time.Until(deadline).Milliseconds()
		if ms > 0 {
			req.Header.Add("Buildkite-Timeout-Milliseconds", strconv.FormatInt(ms, 10))
		}
	}

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

	resp, err := agenthttp.Do(c.logger, c.client, req,
		agenthttp.WithDebugHTTP(c.conf.DebugHTTP),
		agenthttp.WithTraceHTTP(c.conf.TraceHTTP),
	)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	defer io.Copy(io.Discard, resp.Body)

	response := newResponse(resp)

	if err := checkResponse(resp); err != nil {
		// even though there was an error, we still return the response
		// in case the caller wants to inspect it further
		return response, err
	}

	if v != nil {
		if w, ok := v.(io.Writer); ok {
			io.Copy(w, resp.Body)
		} else {
			if strings.Contains(req.Header.Get("Content-Type"), "application/msgpack") {
				return response, errors.New("Msgpack not supported")
			}

			if err = json.NewDecoder(resp.Body).Decode(v); err != nil {
				return response, fmt.Errorf("failed to decode JSON response: %w", err)
			}
		}
	}

	return response, nil
}

// ErrorResponse provides a message.
type ErrorResponse struct {
	Response *http.Response // HTTP response that caused this error
	Message  string         `json:"message"` // error message
}

func (r *ErrorResponse) Error() string {
	s := fmt.Sprintf("%v %v: %s",
		r.Response.Request.Method, r.Response.Request.URL,
		r.Response.Status)

	if r.Message != "" {
		s = fmt.Sprintf("%s: %v", s, r.Message)
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

// Rails doesn't accept dots in some path segments.
func railsPathEscape(s string) string {
	return strings.ReplaceAll(url.PathEscape(s), ".", "%2E")
}
