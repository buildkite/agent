package agent

import (
	"crypto/tls"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"time"

	"github.com/buildkite/agent/api"
	"github.com/buildkite/agent/logger"
)

var (
	debugHTTP = false
)

type APIClientConfig struct {
	Endpoint     string
	Token        string
	DisableHTTP2 bool
}

func APIClientEnableHTTPDebug() {
	debugHTTP = true
}

func NewAPIClient(l logger.Logger, c APIClientConfig) *api.Client {
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

	if c.DisableHTTP2 {
		httpTransport.TLSNextProto = make(map[string]func(string, *tls.Conn) http.RoundTripper)
	}

	// Configure the HTTP client
	httpClient := &http.Client{Transport: &api.AuthenticatedTransport{
		Token:     c.Token,
		Transport: httpTransport,
	}}
	httpClient.Timeout = 60 * time.Second

	u, err := url.Parse(c.Endpoint)
	if err != nil {
		l.Warn("Failed to parse %q: %v", c.Endpoint, err)
	}

	// Create the Buildkite Agent API Client
	client := api.NewClient(httpClient, l)
	client.BaseURL = u
	client.UserAgent = userAgent()
	client.DebugHTTP = debugHTTP

	return client
}

func userAgent() string {
	return "buildkite-agent/" + Version() + "." + BuildVersion() + " (" + runtime.GOOS + "; " + runtime.GOARCH + ")"
}
