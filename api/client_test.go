package api_test

import (
	"context"
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
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/register":
			if got, want := authToken(req), "llamas"; got != want {
				http.Error(rw, fmt.Sprintf("authToken(req) = %q, want %q", got, want), http.StatusUnauthorized)
				return
			}
			rw.WriteHeader(http.StatusOK)
			fmt.Fprint(rw, `{"id":"12-34-56-78-91", "name":"agent-1", "access_token":"alpacas"}`)

		case "/connect":
			if got, want := authToken(req), "alpacas"; got != want {
				http.Error(rw, fmt.Sprintf("authToken(req) = %q, want %q", got, want), http.StatusUnauthorized)
				return
			}
			rw.WriteHeader(http.StatusOK)
			fmt.Fprint(rw, `{}`)

		default:
			http.Error(rw, fmt.Sprintf("not found; method = %q, path = %q", req.Method, req.URL.Path), http.StatusNotFound)
		}
	}))
	defer server.Close()

	// enable HTTP/2.0 to ensure the client can handle defaults to using it
	server.EnableHTTP2 = true
	server.StartTLS()

	ctx := context.Background()

	// Initial client with a registration token
	c := api.NewClient(logger.Discard, api.Config{
		Endpoint:  server.URL,
		Token:     "llamas",
		TLSConfig: &tls.Config{InsecureSkipVerify: true},
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

	if got, want := httpResp.Proto, "HTTP/2.0"; got != want {
		t.Errorf("httpResp.Proto = %q, want %q", got, want)
	}

	// New client with the access token
	c2 := c.FromAgentRegisterResponse(reg)

	// Check a connect works
	if _, err := c2.Connect(ctx); err != nil {
		t.Errorf("c.FromAgentRegisterResponse(regResp).Connect() error = %v", err)
	}
}

func TestFromPing_WithNoChange(t *testing.T) {
	client := api.NewClient(logger.Discard, api.Config{
		Endpoint: "http://localhost:12314",
		Token:    "llamas",
	})

	if client.FromPing(&api.Ping{}) != client {
		t.Errorf("expected unchanged client after ping with no Endpoint or RequestHeaders")
	}
}

func TestFromPing_WithHeaderChange(t *testing.T) {
	client := api.NewClient(logger.Discard, api.Config{
		Endpoint: "http://localhost:12314",
		Token:    "llamas",
	})

	if client.FromPing(&api.Ping{RequestHeaders: map[string]string{"Buildkite-A": "b"}}) == client {
		t.Errorf("expected new client after ping with RequestHeaders")
	}
}

func TestFromPing_WithEndpointChange(t *testing.T) {
	client := api.NewClient(logger.Discard, api.Config{
		Endpoint: "https://agent.buildkite.com/v3",
		Token:    "llamas",
	})

	if client.FromPing(&api.Ping{Endpoint: "https://shard-0.agent.buildkite.com/v3"}) == client {
		t.Errorf("expected new client after ping with RequestHeaders")
	}
}

func authToken(req *http.Request) string {
	return strings.TrimPrefix(req.Header.Get("Authorization"), "Token ")
}
