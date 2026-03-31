package clicommand

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/buildkite/agent/v4/logger"
)

func newArtifactTestServer(t *testing.T) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		switch req.URL.RequestURI() {
		case "/builds/buildid/artifacts/search?query=foo.%2A&state=finished":
			_, _ = io.WriteString(rw, `[{"path": "foo.txt", "sha1sum": "theshastring", "sha256sum": "thesha256string"}]`)
		default:
			t.Errorf("unexpected HTTP request: %s %v", req.Method, req.URL.RequestURI())
		}
	}))
}

func TestSearchAndPrintSha1Sum(t *testing.T) {
	t.Parallel()

	server := newArtifactTestServer(t)
	defer server.Close()

	ctx := context.Background()

	cfg := ArtifactShasumConfig{
		Query: "foo.*",
		Build: "buildid",
		APIConfig: APIConfig{
			AgentAccessToken: "agentaccesstoken",
			Endpoint:         server.URL,
		},
	}
	l := logger.NewBuffer()
	stdout := new(bytes.Buffer)

	if err := searchAndPrintShaSum(ctx, cfg, l, stdout); err != nil {
		t.Fatalf("searchAndPrintShaSum() error = %v", err)
	}

	if got, want := stdout.String(), "theshastring\n"; got != want {
		t.Errorf("stdout.String() = %q, want %q", got, want)
	}

	if got, want := l.Messages, `[info] Searching for artifacts: "foo.*"`; !slices.Contains(got, want) {
		t.Errorf("l.Messages = %v, want containing %q", got, want)
	}
	if got, want := l.Messages, `[debug] Artifact "foo.txt" found`; !slices.Contains(got, want) {
		t.Errorf("l.Messages = %v, want containing %q", got, want)
	}
}

func TestSearchAndPrintSha256Sum(t *testing.T) {
	t.Parallel()

	server := newArtifactTestServer(t)
	defer server.Close()

	ctx := context.Background()

	cfg := ArtifactShasumConfig{
		Query:  "foo.*",
		Build:  "buildid",
		Sha256: true,
		APIConfig: APIConfig{
			AgentAccessToken: "agentaccesstoken",
			Endpoint:         server.URL,
		},
	}
	l := logger.NewBuffer()
	stdout := new(bytes.Buffer)

	if err := searchAndPrintShaSum(ctx, cfg, l, stdout); err != nil {
		t.Fatalf("searchAndPrintShaSum() error = %v", err)
	}

	if got, want := stdout.String(), "thesha256string\n"; got != want {
		t.Errorf("stdout.String() = %q, want %q", got, want)
	}

	if got, want := l.Messages, `[info] Searching for artifacts: "foo.*"`; !slices.Contains(got, want) {
		t.Errorf("l.Messages = %v, want containing %q", got, want)
	}
	if got, want := l.Messages, `[debug] Artifact "foo.txt" found`; !slices.Contains(got, want) {
		t.Errorf("l.Messages = %v, want containing %q", got, want)
	}
}
