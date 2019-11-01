package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/buildkite/agent/v3/logger"
)

func TestRegisteringAndConnectingClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case `/register`:
			if !checkAuthToken(t, req, "llamas") {
				http.Error(rw, "Bad auth", http.StatusUnauthorized)
				return
			}
			rw.WriteHeader(http.StatusOK)
			fmt.Fprintf(rw, `{"id":"12-34-56-78-91", "name":"agent-1", "access_token":"alpacas"}`)

		case `/connect`:
			if !checkAuthToken(t, req, "alpacas") {
				http.Error(rw, "Bad auth", http.StatusUnauthorized)
				return
			}
			rw.WriteHeader(http.StatusOK)
			fmt.Fprintf(rw, `{}`)

		default:
			t.Errorf("Unknown endpoint %s %s", req.Method, req.URL.Path)
			http.Error(rw, "Not found", http.StatusNotFound)
		}
	}))
	defer server.Close()

	// Initial client with a registration token
	c := NewClient(logger.Discard, Config{
		Endpoint: server.URL,
		Token:    "llamas",
	})

	// Check a register works
	regResp, _, err := c.Register(&AgentRegisterRequest{})
	if err != nil {
		t.Fatal(err)
	}

	if regResp.Name != "agent-1" {
		t.Fatalf("Bad name %q", regResp.Name)
	}

	if regResp.AccessToken != "alpacas" {
		t.Fatalf("Bad access token %q", regResp.AccessToken)
	}

	// New client with the access token
	c2 := c.FromAgentRegisterResponse(regResp)

	// Check a connect works
	_, err = c2.Connect()
	if err != nil {
		t.Fatal(err)
	}
}

func checkAuthToken(t *testing.T, req *http.Request, token string) bool {
	t.Helper()
	if auth := req.Header.Get(`Authorization`); auth != fmt.Sprintf("Token %s", token) {
		t.Errorf("Bad Authorization header %q", auth)
		return false
	}
	return true
}
