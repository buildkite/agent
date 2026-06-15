package clicommand

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/urfave/cli"
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

	app := cli.NewApp()
	app.Commands = []cli.Command{OIDCRequestTokenCommand}

	var out bytes.Buffer
	app.Writer = &out

	runArgs := []string{
		"buildkite-agent",
		"request-token",
		"--endpoint", serverURL,
		"--agent-access-token", "agent-access-token",
		"--job", "job-123",
	}
	runArgs = append(runArgs, args...)

	err := app.Run(runArgs)
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
			"OIDC token was not printed",
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
