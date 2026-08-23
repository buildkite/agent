package api_test

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/roko"
)

const gatewayErrorPage = `<html>
<head><title>502 Bad Gateway</title></head>
<body><center><h1>502 Bad Gateway</h1></center></body>
</html>`

// An intermediary (proxy, load balancer, CDN) answering with an HTML error page
// and a success status is indistinguishable from the API returning nonsense. The
// error has to name the request and what came back, otherwise all a job log gets
// is "invalid character '<' looking for beginning of value".
func TestOIDCTokenErrorDescribesUndecodableResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.Header().Set("Content-Type", "text/html; charset=utf-8")
		rw.WriteHeader(http.StatusOK)
		fmt.Fprint(rw, gatewayErrorPage) //nolint:errcheck // The test would still fail
	}))
	defer server.Close()

	c := api.NewClient(slog.New(slog.DiscardHandler), api.Config{Endpoint: server.URL, Token: "llamas"})
	_, resp, err := c.OIDCToken(t.Context(), &api.OIDCTokenRequest{Job: "job-123"})
	if err == nil {
		t.Fatal("c.OIDCToken(...) error = nil, want non-nil")
	}

	var undecodable *api.UndecodableResponseError
	if !errors.As(err, &undecodable) {
		t.Fatalf("c.OIDCToken(...) error = %v (%T), want an *api.UndecodableResponseError", err, err)
	}

	for _, want := range []string{
		"POST",
		server.URL + "/jobs/job-123/oidc/tokens",
		"200 OK",
		"text/html",
		"502 Bad Gateway", // the snippet
	} {
		if got := err.Error(); !strings.Contains(got, want) {
			t.Errorf("c.OIDCToken(...) error = %q, want containing %q", got, want)
		}
	}

	// The response is still handed back for callers that inspect it.
	if resp == nil {
		t.Fatal("c.OIDCToken(...) response = nil, want non-nil")
	}
	if got, want := resp.StatusCode, http.StatusOK; got != want {
		t.Errorf("resp.StatusCode = %d, want %d", got, want)
	}
}

// A body that claims to be JSON but doesn't parse came from the API itself, so it
// can hold credentials. Report everything except the body.
func TestUndecodableJSONResponseErrorOmitsBody(t *testing.T) {
	t.Parallel()

	const secret = "bkaj_supersecrettoken"

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		rw.WriteHeader(http.StatusOK)
		fmt.Fprintf(rw, `{"token":%q`, secret) //nolint:errcheck // Deliberately truncated JSON
	}))
	defer server.Close()

	c := api.NewClient(slog.New(slog.DiscardHandler), api.Config{Endpoint: server.URL, Token: "llamas"})
	_, _, err := c.OIDCToken(t.Context(), &api.OIDCTokenRequest{Job: "job-123"})
	if err == nil {
		t.Fatal("c.OIDCToken(...) error = nil, want non-nil")
	}

	if got := err.Error(); strings.Contains(got, secret) {
		t.Errorf("c.OIDCToken(...) error = %q, want it to not contain the response body", got)
	}

	for _, want := range []string{"200 OK", "application/json"} {
		if got := err.Error(); !strings.Contains(got, want) {
			t.Errorf("c.OIDCToken(...) error = %q, want containing %q", got, want)
		}
	}
}

// Non-2xx responses go down a different path (checkResponse), which used to
// discard the body entirely.
func TestErrorResponseIncludesNonJSONBody(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.Header().Set("Content-Type", "text/html; charset=utf-8")
		rw.WriteHeader(http.StatusBadGateway)
		fmt.Fprint(rw, gatewayErrorPage) //nolint:errcheck // The test would still fail
	}))
	defer server.Close()

	c := api.NewClient(slog.New(slog.DiscardHandler), api.Config{Endpoint: server.URL, Token: "llamas"})
	_, _, err := c.OIDCToken(t.Context(), &api.OIDCTokenRequest{Job: "job-123"})
	if err == nil {
		t.Fatal("c.OIDCToken(...) error = nil, want non-nil")
	}

	for _, want := range []string{"502 Bad Gateway", "text/html"} {
		if got := err.Error(); !strings.Contains(got, want) {
			t.Errorf("c.OIDCToken(...) error = %q, want containing %q", got, want)
		}
	}
}

func TestErrorResponseNonJSONBodyIsCapped(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.Header().Set("Content-Type", "text/html")
		rw.WriteHeader(http.StatusBadGateway)
		fmt.Fprint(rw, "<html>"+strings.Repeat("x", 4096)+"</html>") //nolint:errcheck // The test would still fail
	}))
	defer server.Close()

	c := api.NewClient(slog.New(slog.DiscardHandler), api.Config{Endpoint: server.URL, Token: "llamas"})
	_, _, err := c.OIDCToken(t.Context(), &api.OIDCTokenRequest{Job: "job-123"})
	if err == nil {
		t.Fatal("c.OIDCToken(...) error = nil, want non-nil")
	}

	// The whole message, not just the snippet, has to stay log-sized.
	if got, want := len(err.Error()), 1024; got > want {
		t.Errorf("len(c.OIDCToken(...) error) = %d, want <= %d: %q", got, want, err)
	}
	if got, want := err.Error(), "…"; !strings.Contains(got, want) {
		t.Errorf("c.OIDCToken(...) error = %q, want containing a truncation marker %q", got, want)
	}
}

// A JSON error body from the API keeps its message, and gains no snippet.
func TestErrorResponseKeepsAPIMessage(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		rw.WriteHeader(http.StatusUnprocessableEntity)
		fmt.Fprint(rw, `{"message":"audience is not allowed"}`) //nolint:errcheck // The test would still fail
	}))
	defer server.Close()

	c := api.NewClient(slog.New(slog.DiscardHandler), api.Config{Endpoint: server.URL, Token: "llamas"})
	_, _, err := c.OIDCToken(t.Context(), &api.OIDCTokenRequest{Job: "job-123"})
	if err == nil {
		t.Fatal("c.OIDCToken(...) error = nil, want non-nil")
	}

	if got, want := err.Error(), "audience is not allowed"; !strings.Contains(got, want) {
		t.Errorf("c.OIDCToken(...) error = %q, want containing %q", got, want)
	}
	if got, want := err.Error(), "non-JSON body"; strings.Contains(got, want) {
		t.Errorf("c.OIDCToken(...) error = %q, want it to not contain %q", got, want)
	}
}

// A big error page shouldn't dump kilobytes into a job log.
func TestUndecodableResponseSnippetIsCapped(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.Header().Set("Content-Type", "text/html")
		rw.WriteHeader(http.StatusOK)
		fmt.Fprint(rw, "<html>"+strings.Repeat("x", 4096)+"</html>") //nolint:errcheck // The test would still fail
	}))
	defer server.Close()

	c := api.NewClient(slog.New(slog.DiscardHandler), api.Config{Endpoint: server.URL, Token: "llamas"})
	_, _, err := c.OIDCToken(t.Context(), &api.OIDCTokenRequest{Job: "job-123"})

	var undecodable *api.UndecodableResponseError
	if !errors.As(err, &undecodable) {
		t.Fatalf("c.OIDCToken(...) error = %v (%T), want an *api.UndecodableResponseError", err, err)
	}

	if got, want := len(undecodable.Snippet), 512; got > want {
		t.Errorf("len(undecodable.Snippet) = %d, want <= %d", got, want)
	}
	if got, want := undecodable.Snippet, "<html>"; !strings.HasPrefix(got, want) {
		t.Errorf("undecodable.Snippet = %q, want it to start with %q", got, want)
	}
}

func TestUndecodableResponseIsRetryable(t *testing.T) {
	t.Parallel()

	err := error(&api.UndecodableResponseError{
		Method: "POST",
		URL:    "https://agent.buildkite.com/v3/jobs/abc/oidc/tokens",
		Status: "200 OK",
		Err:    errors.New("invalid character '<' looking for beginning of value"),
	})

	if !api.IsRetryableError(err) {
		t.Error("api.IsRetryableError(&UndecodableResponseError{}) = false, want true")
	}

	if !api.IsRetryableError(fmt.Errorf("requesting OIDC token: %w", err)) {
		t.Error("api.IsRetryableError(wrapped UndecodableResponseError) = false, want true")
	}

	// The success status must not be read as "this request is settled": the whole
	// point is that we never got a usable answer.
	retrier := roko.NewRetrier(roko.WithMaxAttempts(3))
	resp := &api.Response{Response: &http.Response{StatusCode: http.StatusOK}}
	if api.BreakOnNonRetryable(retrier, resp, err) {
		t.Error("api.BreakOnNonRetryable(...) = true, want false for an undecodable 200 response")
	}
}
