package agent

import (
	"net"
	"net/http"
	"net/url"
	"runtime"
	"time"

	"golang.org/x/net/http2"
	"github.com/buildkite/agent/api"
)

var debug = false

type APIClient struct {
	Endpoint string
	Token    string
}

func APIClientEnableHTTPDebug() {
	debug = true
}

func (a APIClient) Create() *api.Client {
	httpTransport := &http.Transport{
		Proxy:              http.ProxyFromEnvironment,
		DisableKeepAlives:  false,
		DisableCompression: false,
		Dial: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).Dial,
		TLSHandshakeTimeout: 30 * time.Second,
	}
	http2.ConfigureTransport(httpTransport)

	// Create the transport used when making the Buildkite Agent API calls
	transport := &api.AuthenticatedTransport{
		Token: a.Token,
		Transport: httpTransport,
	}

	// From the transport, create the a http client
	httpClient := transport.Client()
	httpClient.Timeout = 60 * time.Second

	// Create the Buildkite Agent API Client
	client := api.NewClient(httpClient)
	client.BaseURL, _ = url.Parse(a.Endpoint)
	client.UserAgent = a.UserAgent()
	client.DebugHTTP = debug

	return client
}

func (a APIClient) UserAgent() string {
	return "buildkite-agent/" + Version() + " (" + runtime.GOOS + "; " + runtime.GOARCH + ")"
}
