package agent

import (
	"crypto/sha256"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/buildkite/agent/api"
	"github.com/buildkite/agent/logger"
)

// APIProxy provides either a unix socket or a tcp socket listener with a proxy
// that will authenticate to the Buildkite Agent API
type APIProxy struct {
	upstreamToken    string
	upstreamEndpoint string
	token            string
	socket           *os.File
	listener         net.Listener
	listenerWg       *sync.WaitGroup
}

func NewAPIProxy(endpoint string, token string) *APIProxy {
	var wg sync.WaitGroup
	wg.Add(1)

	return &APIProxy{
		upstreamToken:    token,
		upstreamEndpoint: endpoint,
		token:            fmt.Sprintf("%x", sha256.Sum256([]byte(string(time.Now().UnixNano())))),
		listenerWg:       &wg,
	}
}

// Listen on either a tcp socket (for windows) or a unix socket
func (p *APIProxy) Listen() error {
	defer p.listenerWg.Done()
	var err error

	// windows doesn't support unix sockets, so we fall back to a tcp socket
	if runtime.GOOS == `windows` {
		p.listener, err = p.listenOnTCPSocket()
	} else {
		p.listener, p.socket, err = p.listenOnUnixSocket()
	}

	if err != nil {
		return err
	}

	endpoint, err := url.Parse(p.upstreamEndpoint)
	if err != nil {
		return err
	}

	go func() {
		proxy := httputil.NewSingleHostReverseProxy(endpoint)
		proxy.Transport = &api.AuthenticatedTransport{Token: p.upstreamToken}

		// customize the reverse proxy director so that we can make some changes to the request
		director := proxy.Director
		proxy.Director = func(req *http.Request) {
			director(req)

			// set the host header whilst proxying
			req.Host = req.URL.Host
		}

		// serve traffic, proxy off to the reverse proxy
		_ = http.Serve(p.listener, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			if r.Header.Get(`Authorization`) != `Token `+p.token {
				http.Error(rw, "Invalid authorization token", http.StatusBadRequest)
				return
			}
			proxy.ServeHTTP(rw, r)
		}))
	}()

	return nil
}

func (p *APIProxy) listenOnUnixSocket() (net.Listener, *os.File, error) {
	socket, err := ioutil.TempFile("", "agent-socket")
	if err != nil {
		return nil, nil, err
	}

	// Servers should unlink the socket path name prior to binding it.
	// https://troydhanson.github.io/network/Unix_domain_sockets.html
	_ = os.Remove(socket.Name())

	logger.Debug("[APIProxy] Listening on unix socket %s", socket.Name())

	// create a unix socket to do the listening
	l, err := net.Listen("unix", socket.Name())
	if err != nil {
		return nil, nil, err
	}

	// Restrict to owner r+w permissions
	if err = os.Chmod(socket.Name(), 0600); err != nil {
		return nil, nil, err
	}

	return l, socket, nil
}

func (p *APIProxy) listenOnTCPSocket() (net.Listener, error) {
	// Listen on the first available non-privileged port
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}

	logger.Debug("[APIProxy] Listening on tcp socket %s", l.Addr().String())
	return l, nil
}

// Close any listeners or internal files
func (p *APIProxy) Close() error {
	defer p.listenerWg.Add(1)
	return p.listener.Close()
}

// Wait blocks until the listener is ready
func (p *APIProxy) Wait() {
	p.listenerWg.Wait()
}

func (p *APIProxy) Endpoint() string {
	if p.socket != nil {
		return `unix://` + p.listener.Addr().String()
	}
	return `http://` + p.listener.Addr().String()
}

func (p *APIProxy) AccessToken() string {
	return p.token
}
