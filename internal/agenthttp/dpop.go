package agenthttp

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/buildkite/agent/v4/internal/identity"
)

// WithDPoP returns a copy of client whose transport signs requests with key.
// Enrollment uses Token auth without ath; bound credentials use DPoP auth and ath.
// Credentials and signatures never follow redirects to another origin.
func WithDPoP(client *http.Client, token string, key *identity.Key, endpoint string, bound bool) *http.Client {
	transport := client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	copy := *client
	copy.Transport = &dpopTransport{base: transport, token: token, key: key, endpoint: endpoint, bound: bound}
	return &copy
}

type dpopTransport struct {
	base     http.RoundTripper
	token    string
	key      *identity.Key
	endpoint string
	bound    bool
	mu       sync.Mutex
	nonce    string
}

func (t *dpopTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.key == nil || t.token == "" {
		if req.Body != nil {
			_ = req.Body.Close()
		}
		return nil, fmt.Errorf("host-bound credentials require a host key and token")
	}
	endpoint, err := url.Parse(t.endpoint)
	if err != nil || endpoint.Scheme != "https" || endpoint.User != nil ||
		req.URL.Scheme != "https" || req.URL.User != nil ||
		!strings.EqualFold(req.URL.Host, endpoint.Host) ||
		(req.Host != "" && !strings.EqualFold(req.Host, req.URL.Host)) {
		if req.Body != nil {
			_ = req.Body.Close()
		}
		return nil, fmt.Errorf("host-bound credentials require HTTPS to the configured API origin")
	}

	for attempt := 0; ; attempt++ {
		r := req.Clone(req.Context())
		if attempt > 0 && req.Body != nil {
			r.Body, err = req.GetBody()
			if err != nil {
				return nil, fmt.Errorf("replay DPoP nonce challenge: %w", err)
			}
		}
		t.mu.Lock()
		nonce := t.nonce
		t.mu.Unlock()
		scheme, proofToken := "Token", ""
		if t.bound {
			scheme, proofToken = "DPoP", t.token
		}
		proof, err := t.key.Proof(r.Method, r.URL, proofToken, nonce)
		if err != nil {
			if r.Body != nil {
				_ = r.Body.Close()
			}
			return nil, err
		}
		r.Header.Set("Authorization", scheme+" "+t.token)
		r.Header.Set("DPoP", proof)
		resp, err := t.base.RoundTrip(r)
		if err != nil {
			return resp, err
		}
		if nextNonce := resp.Header.Get("DPoP-Nonce"); nextNonce != "" {
			t.mu.Lock()
			t.nonce = nextNonce
			t.mu.Unlock()
			if attempt == 0 && (resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusUnauthorized) &&
				(req.Body == nil || req.GetBody != nil) {
				_ = resp.Body.Close()
				continue
			}
		}
		return resp, nil
	}
}

func (t *dpopTransport) CloseIdleConnections() {
	if closer, ok := t.base.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}
