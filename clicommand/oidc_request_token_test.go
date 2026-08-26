package clicommand

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/buildkite/agent/v4/api"
	"github.com/urfave/cli/v3"
)

func newOIDCRequestTokenTestServer(t *testing.T, jobID, token string) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		wantPath := fmt.Sprintf("/jobs/%s/oidc/tokens", url.PathEscape(jobID))
		if req.URL.Path != wantPath {
			http.Error(rw, fmt.Sprintf("got path %q, want %q", req.URL.Path, wantPath), http.StatusNotFound)
			return
		}

		_, _ = fmt.Fprintf(rw, `{"token":%q}`, token)
	}))
}

func runOIDCRequestTokenCommand(t *testing.T, serverURL string, args ...string) (string, error) {
	t.Helper()

	// Clone the command, because Run mutates it, which breaks the command completeness test.
	cmd := *OIDCRequestTokenCommand
	var out bytes.Buffer
	app := &cli.Command{
		Name:      "buildkite-agent",
		Commands:  []*cli.Command{&cmd},
		Writer:    &out,
		ErrWriter: os.Stderr,
	}

	runArgs := []string{
		"buildkite-agent",
		"request-token",
		"--endpoint", serverURL,
		"--agent-access-token", "agent-access-token",
		"--job", "job-123",
	}
	runArgs = append(runArgs, args...)

	err := app.Run(t.Context(), runArgs)
	return out.String(), err
}

func TestOIDCRequestToken(t *testing.T) {
	const oidcToken = "oidc-token"

	server := newOIDCRequestTokenTestServer(t, "job-123", oidcToken)
	defer server.Close()

	t.Run("requires explicit opt out when redaction cannot be set up", func(t *testing.T) {
		t.Setenv("BUILDKITE_AGENT_JOB_API_SOCKET", "")
		t.Setenv("BUILDKITE_AGENT_JOB_API_TOKEN", "")

		out, err := runOIDCRequestTokenCommand(t, server.URL)
		if err == nil {
			t.Fatal("runOIDCRequestTokenCommand() error = nil, want non-nil")
		}

		if out != "" {
			t.Fatalf("runOIDCRequestTokenCommand() output = %q, want empty", out)
		}

		for _, want := range []string{
			"automatic OIDC token redaction requires the Job API",
			"could leak in logs without redaction",
			"--skip-redaction",
			"BUILDKITE_AGENT_OIDC_REQUEST_TOKEN_SKIP_TOKEN_REDACTION=true",
		} {
			if got := err.Error(); !strings.Contains(got, want) {
				t.Fatalf("runOIDCRequestTokenCommand() error = %q, want containing %q", got, want)
			}
		}
	})

	t.Run("prints token when redaction is explicitly skipped", func(t *testing.T) {
		t.Setenv("BUILDKITE_AGENT_JOB_API_SOCKET", "")
		t.Setenv("BUILDKITE_AGENT_JOB_API_TOKEN", "")

		out, err := runOIDCRequestTokenCommand(t, server.URL, "--skip-redaction")
		if err != nil {
			t.Fatalf("runOIDCRequestTokenCommand() error = %v, want nil", err)
		}

		if want := oidcToken + "\n"; out != want {
			t.Fatalf("runOIDCRequestTokenCommand() output = %q, want %q", out, want)
		}
	})
}

// A non-retryable 4xx means the API positively refused to issue a token, so
// the command must not retry, and must exit with the documented distinct
// status so callers (e.g. test-engine-client) can tell a refusal apart from a
// transient failure without parsing stderr.
func TestOIDCRequestTokenRefusalExitsWithDistinctStatus(t *testing.T) {
	var requests atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		requests.Add(1)
		rw.Header().Set("Content-Type", "application/json")
		rw.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = fmt.Fprint(rw, `{"message":"audience not allowed"}`)
	}))
	defer server.Close()

	_, err := runOIDCRequestTokenCommand(t, server.URL)
	if err == nil {
		t.Fatal("runOIDCRequestTokenCommand() error = nil, want non-nil")
	}

	eerr := new(ExitError)
	if !errors.As(err, &eerr) {
		t.Fatalf("runOIDCRequestTokenCommand() error = %v (%T), want ExitError", err, err)
	}
	if got, want := eerr.Code(), OIDCTokenRefusedExitStatus; got != want {
		t.Errorf("ExitError.Code() = %d, want %d", got, want)
	}
	if got, want := err.Error(), "audience not allowed"; !strings.Contains(got, want) {
		t.Errorf("error = %q, want containing %q", got, want)
	}

	if got, want := requests.Load(), int32(1); got != want {
		t.Errorf("server received %d requests, want %d (refusals must not be retried)", got, want)
	}
}

func TestOIDCTokenRefused(t *testing.T) {
	errorResponse := func(status int) error {
		return &api.ErrorResponse{
			Response: &http.Response{
				StatusCode: status,
				Request: &http.Request{
					Method: "POST",
					URL:    &url.URL{Scheme: "https", Host: "agent.buildkite.com", Path: "/v3/jobs/job-123/oidc/tokens"},
				},
			},
			Message: "message",
		}
	}

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "401 unauthorized", err: errorResponse(http.StatusUnauthorized), want: true},
		{name: "403 forbidden", err: errorResponse(http.StatusForbidden), want: true},
		{name: "422 unprocessable", err: errorResponse(http.StatusUnprocessableEntity), want: true},
		{name: "wrapped 422", err: fmt.Errorf("could not obtain OIDC token: %w", errorResponse(http.StatusUnprocessableEntity)), want: true},
		{name: "408 request timeout", err: errorResponse(http.StatusRequestTimeout), want: false},
		{name: "425 too early", err: errorResponse(http.StatusTooEarly), want: false},
		{name: "429 too many requests", err: errorResponse(http.StatusTooManyRequests), want: false},
		{name: "500 internal server error", err: errorResponse(http.StatusInternalServerError), want: false},
		{name: "504 gateway timeout", err: errorResponse(http.StatusGatewayTimeout), want: false},
		{name: "transport error", err: errors.New("net/http: request canceled"), want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := oidcTokenRefused(test.err); got != test.want {
				t.Errorf("oidcTokenRefused(%v) = %t, want %t", test.err, got, test.want)
			}
		})
	}
}

// An intermediary in front of the API can answer with an HTML error page and a
// 2xx status. That used to fail the job on the first attempt, because a success
// status was read as "settled", so the retrier broke instead of retrying.
func TestOIDCRequestTokenRetriesUndecodableResponse(t *testing.T) {
	t.Setenv("BUILDKITE_AGENT_JOB_API_SOCKET", "")
	t.Setenv("BUILDKITE_AGENT_JOB_API_TOKEN", "")

	const oidcToken = "oidc-token"
	var requests atomic.Int32

	// The first attempt gets a gateway error page with a 200; the second gets the
	// real thing. Note the retrier's first backoff is a real 1s sleep.
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if requests.Add(1) == 1 {
			rw.Header().Set("Content-Type", "text/html")
			rw.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(rw, "<html><head><title>502 Bad Gateway</title></head></html>")
			return
		}
		_, _ = fmt.Fprintf(rw, `{"token":%q}`, oidcToken)
	}))
	defer server.Close()

	out, err := runOIDCRequestTokenCommand(t, server.URL, "--skip-redaction")
	if err != nil {
		t.Fatalf("runOIDCRequestTokenCommand() error = %v, want nil", err)
	}

	if want := oidcToken + "\n"; out != want {
		t.Fatalf("runOIDCRequestTokenCommand() output = %q, want %q", out, want)
	}

	if got, want := requests.Load(), int32(2); got != want {
		t.Errorf("server received %d requests, want %d", got, want)
	}
}
