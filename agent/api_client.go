package agent

import (
	"bufio"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"time"

	"github.com/buildkite/agent/api"
	"github.com/buildkite/agent/logger"
)

var debug = false

type APIClient struct {
	Endpoint     string
	Token        string
	DisableHTTP2 bool
}

func APIClientEnableHTTPDebug() {
	debug = true
}

func (a APIClient) Create() *api.Client {
	u, err := url.Parse(a.Endpoint)
	if err != nil {
		logger.Warn("Failed to parse %q: %v", a.Endpoint, err)
	}

	if u != nil && u.Scheme == `unix` {
		return a.createFromSocket(u.Path)
	}

	httpTransport := &http.Transport{
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

	if a.DisableHTTP2 {
		httpTransport.TLSNextProto = make(map[string]func(string, *tls.Conn) http.RoundTripper)
	}

	// Configure the HTTP client
	httpClient := &http.Client{Transport: &api.AuthenticatedTransport{
		Token:     a.Token,
		Transport: httpTransport,
	}}
	httpClient.Timeout = 60 * time.Second

	// Create the Buildkite Agent API Client
	client := api.NewClient(httpClient)
	client.BaseURL, _ = url.Parse(a.Endpoint)
	client.UserAgent = a.UserAgent()
	client.DebugHTTP = debug

	return client
}

func (a APIClient) createFromSocket(socket string) *api.Client {
	httpClient := &http.Client{
		Transport: &api.AuthenticatedTransport{
			Token: a.Token,
			Transport: &socketTransport{
				Socket:      socket,
				DialTimeout: 30 * time.Second,
			},
		},
	}

	// Create the Buildkite Agent API Client
	client := api.NewClient(httpClient)
	client.BaseURL, _ = url.Parse(`http+unix://buildkite-agent`)
	client.UserAgent = a.UserAgent()
	client.DebugHTTP = debug

	return client
}

func (a APIClient) UserAgent() string {
	return "buildkite-agent/" + Version() + "." + BuildVersion() + " (" + runtime.GOOS + "; " + runtime.GOARCH + ")"
}

// Transport is a http.RoundTripper that connects to Unix domain sockets.
type socketTransport struct {
	DialTimeout           time.Duration
	RequestTimeout        time.Duration
	ResponseHeaderTimeout time.Duration
	Socket                string
}

// RoundTrip executes a single HTTP transaction. See net/http.RoundTripper.
func (t *socketTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL == nil {
		return nil, errors.New("http+unix: nil Request.URL")
	}
	if req.URL.Scheme != `http+unix` {
		return nil, errors.New("unsupported protocol scheme: " + req.URL.Scheme)
	}
	if req.URL.Host == "" {
		return nil, errors.New("http+unix: no Host in request URL")
	}

	c, err := net.DialTimeout("unix", t.Socket, t.DialTimeout)
	if err != nil {
		return nil, err
	}
	r := bufio.NewReader(c)
	if t.RequestTimeout > 0 {
		c.SetWriteDeadline(time.Now().Add(t.RequestTimeout))
	}
	if err := req.Write(c); err != nil {
		return nil, err
	}
	if t.ResponseHeaderTimeout > 0 {
		c.SetReadDeadline(time.Now().Add(t.ResponseHeaderTimeout))
	}
	resp, err := http.ReadResponse(r, req)
	return resp, err
}
