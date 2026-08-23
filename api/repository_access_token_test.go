package api_test

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/buildkite/agent/v4/api"
)

func TestGenerateRepositoryAccessToken(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if got, want := req.Method, http.MethodPost; got != want {
			t.Errorf("method = %q, want %q", got, want)
		}
		if got, want := req.URL.EscapedPath(), "/jobs/job%2Fid/repository_access_token"; got != want {
			t.Errorf("escaped path = %q, want %q", got, want)
		}

		var body api.RepositoryAccessTokenRequest
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if got, want := body.RepoURL, "https://git.example.com/acme/widgets.git"; got != want {
			t.Errorf("repo_url = %q, want %q", got, want)
		}

		if attempts.Add(1) == 1 {
			rw.Header().Set("Retry-After", "0")
			http.Error(rw, `{"message":"retry"}`, http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(rw, `{"token":"repository-token"}`) //nolint:errcheck
	}))
	t.Cleanup(server.Close)

	client := api.NewClient(slog.New(slog.DiscardHandler), api.Config{Endpoint: server.URL, Token: "agent-token"})
	token, response, err := client.GenerateRepositoryAccessToken(
		t.Context(),
		"https://git.example.com/acme/widgets.git",
		"job/id",
	)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := token, "repository-token"; got != want {
		t.Errorf("token = %q, want %q", got, want)
	}
	if got, want := response.StatusCode, http.StatusOK; got != want {
		t.Errorf("status = %d, want %d", got, want)
	}
	if got, want := attempts.Load(), int32(2); got != want {
		t.Errorf("attempts = %d, want %d", got, want)
	}
}

func TestGenerateGithubCodeAccessTokenUsesRepositoryEndpoint(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if got, want := req.URL.Path, "/jobs/job-id/repository_access_token"; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		fmt.Fprint(rw, `{"token":"repository-token"}`) //nolint:errcheck
	}))
	t.Cleanup(server.Close)

	client := api.NewClient(slog.New(slog.DiscardHandler), api.Config{Endpoint: server.URL, Token: "agent-token"})
	token, _, err := client.GenerateGithubCodeAccessToken(
		t.Context(),
		"https://git.example.com/acme/widgets.git",
		"job-id",
	)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := token, "repository-token"; got != want {
		t.Errorf("token = %q, want %q", got, want)
	}
}
