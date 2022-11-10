package api_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/logger"
)

func TestOidcTokenNoAudience(t *testing.T) {
	const jobId = "b078e2d2-86e9-4c12-bf3b-612a8058d0a4"
	const oidcToken = "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiYWRtaW4iOnRydWUsImlhdCI6MTUxNjIzOTAyMn0.NHVaYe26MbtOYhSKkoKYdFVomg4i8ZJd8_-RU8VNbftc4TSMb4bXP3l3YlNWACwyXPGffz5aXHc6lty1Y2t4SWRqGteragsVdZufDn5BlnJl9pdR_kdVFUsra2rWKEofkZeIC4yWytE58sMIihvo9H1ScmmVwBcQP6XETqYd0aSHp1gOa9RdUPDvoXQ5oqygTqVtxaDr6wUFKrKItgBMzWIdNZ6y7O9E0DhEPTbE9rfBo6KTFsHAZnMg4k68CDp2woYIaXbmYTWcvbzIuHO7_37GT79XdIwkm95QJ7hYC9RiwrV7mesbY4PAahERJawntho0my942XheVLmGwLMBkQ"
	const accessToken = "llamas"

	path := fmt.Sprintf("/jobs/%s/oidc/tokens", jobId)
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case path:
			if got, want := authToken(req), accessToken; got != want {
				http.Error(
					rw,
					fmt.Sprintf("authToken(req) = %q, want %q", got, want),
					http.StatusUnauthorized,
				)
				return
			}
			rw.WriteHeader(http.StatusOK)
			fmt.Fprint(rw, fmt.Sprintf(`{"token":"%s"}`, oidcToken))

		default:
			http.Error(
				rw,
				fmt.Sprintf(
					`{"message": "not found; method = %q, path = %q"}`,
					req.Method,
					req.URL.Path,
				),
				http.StatusNotFound,
			)
		}
	}))
	defer server.Close()

	// Initial client with a registration token
	client := api.NewClient(logger.Discard, api.Config{
		UserAgent: "Test",
		Endpoint:  server.URL,
		Token:     accessToken,
		DebugHTTP: true,
	})

	if token, resp, err := client.OidcToken(jobId); err != nil {
		t.Fatalf("OidcToken(%v) error = %# v", jobId, err)
	} else if token.Token != oidcToken {
		t.Fatalf("OidcToken(%v) got token = %# v, want %# v", jobId, token, &api.OidcToken{Token: oidcToken})
	} else if resp.StatusCode != http.StatusOK {
		t.Fatalf("OidcToken(%v) got StatusCode = %# v, want %# v", jobId, resp.StatusCode, http.StatusOK)
	}
}
