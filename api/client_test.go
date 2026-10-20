package api_test

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/logger"
)

func TestRegisteringAndConnectingClient(t *testing.T) {
	for _, test := range []struct {
		name         string
		serverHTTP2  bool
		disableHTTP2 bool
		wantProto    string
	}{
		{name: "HTTP2", serverHTTP2: true, wantProto: "HTTP/2.0"},
		{name: "HTTP2 disabled", serverHTTP2: true, disableHTTP2: true, wantProto: "HTTP/1.1"},
		{name: "HTTP1 fallback", wantProto: "HTTP/1.1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
				switch req.URL.Path {
				case "/register":
					if got, want := authToken(req), "llamas"; got != want {
						http.Error(rw, fmt.Sprintf("authToken(req) = %q, want %q", got, want), http.StatusUnauthorized)
						return
					}
					rw.WriteHeader(http.StatusOK)
					fmt.Fprint(rw, `{"id":"12-34-56-78-91", "name":"agent-1", "access_token":"alpacas"}`) //nolint:errcheck // The test would still fail

				case "/connect":
					if got, want := authToken(req), "alpacas"; got != want {
						http.Error(rw, fmt.Sprintf("authToken(req) = %q, want %q", got, want), http.StatusUnauthorized)
						return
					}
					rw.WriteHeader(http.StatusOK)
					fmt.Fprint(rw, `{}`) //nolint:errcheck // The test would still fail

				default:
					http.Error(rw, fmt.Sprintf("not found; method = %q, path = %q", req.Method, req.URL.Path), http.StatusNotFound)
				}
			}))
			defer server.Close()

			server.EnableHTTP2 = test.serverHTTP2
			server.StartTLS()

			ctx := t.Context()

			// Initial client with a registration token
			c := api.NewClient(logger.Discard, api.Config{
				Endpoint: server.URL,
				Token:    "llamas",
				TLSConfig: &tls.Config{
					InsecureSkipVerify: true,
					NextProtos:         []string{"h2", "http/1.1"},
				},
				DisableHTTP2: test.disableHTTP2,
			})

			// Check a register works
			reg, httpResp, err := c.Register(ctx, &api.AgentRegisterRequest{})
			if err != nil {
				t.Fatalf("c.Register(&AgentRegisterRequest{}) error = %v", err)
			}

			if got, want := reg.Name, "agent-1"; got != want {
				t.Errorf("regResp.Name = %q, want %q", got, want)
			}

			if got, want := reg.AccessToken, "alpacas"; got != want {
				t.Errorf("regResp.AccessToken = %q, want %q", got, want)
			}

			if got, want := httpResp.Proto, test.wantProto; got != want {
				t.Errorf("httpResp.Proto = %q, want %q", got, want)
			}

			// New client with the access token
			c2 := c.FromAgentRegisterResponse(reg)

			// Check a connect works
			if httpResp, err := c2.Connect(ctx); err != nil {
				t.Errorf("c.FromAgentRegisterResponse(regResp).Connect() error = %v", err)
			} else if got, want := httpResp.Proto, test.wantProto; got != want {
				t.Errorf("httpResp.Proto = %q, want %q", got, want)
			}
		})
	}
}

func authToken(req *http.Request) string {
	return strings.TrimPrefix(req.Header.Get("Authorization"), "Token ")
}
