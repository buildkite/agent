package clicommand

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"

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
