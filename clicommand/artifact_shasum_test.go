package clicommand

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/buildkite/agent/v4/internal/logtest"
)

func findLogAttr(records []slog.Record, message, key string) (any, bool) {
	for _, record := range records {
		if record.Message != message {
			continue
		}
		var value any
		record.Attrs(func(attr slog.Attr) bool {
			if attr.Key == key {
				value = attr.Value.Any()
				return false
			}
			return true
		})
		return value, value != nil
	}
	return nil, false
}

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

	ctx := t.Context()

	cfg := ArtifactShasumConfig{
		Query: "foo.*",
		Build: "buildid",
		APIConfig: APIConfig{
			AgentAccessToken: "agentaccesstoken",
			Endpoint:         server.URL,
		},
	}
	l, lh := logtest.NewLogger()
	stdout := new(bytes.Buffer)

	if err := searchAndPrintShaSum(ctx, cfg, l, stdout); err != nil {
		t.Fatalf("searchAndPrintShaSum() error = %v", err)
	}

	if got, want := stdout.String(), "theshastring\n"; got != want {
		t.Errorf("stdout.String() = %q, want %q", got, want)
	}

	if got, ok := findLogAttr(lh.Records(), "Searching for artifacts", "query"); !ok || got != "foo.*" {
		t.Errorf("search query attr = %v, %t, want %q, true", got, ok, "foo.*")
	}
	if got, ok := findLogAttr(lh.Records(), "Artifact found", "path"); !ok || got != "foo.txt" {
		t.Errorf("artifact path attr = %v, %t, want %q, true", got, ok, "foo.txt")
	}
}

func TestSearchAndPrintSha256Sum(t *testing.T) {
	t.Parallel()

	server := newArtifactTestServer(t)
	defer server.Close()

	ctx := t.Context()

	cfg := ArtifactShasumConfig{
		Query:  "foo.*",
		Build:  "buildid",
		Sha256: true,
		APIConfig: APIConfig{
			AgentAccessToken: "agentaccesstoken",
			Endpoint:         server.URL,
		},
	}
	l, lh := logtest.NewLogger()
	stdout := new(bytes.Buffer)

	if err := searchAndPrintShaSum(ctx, cfg, l, stdout); err != nil {
		t.Fatalf("searchAndPrintShaSum() error = %v", err)
	}

	if got, want := stdout.String(), "thesha256string\n"; got != want {
		t.Errorf("stdout.String() = %q, want %q", got, want)
	}

	if got, ok := findLogAttr(lh.Records(), "Searching for artifacts", "query"); !ok || got != "foo.*" {
		t.Errorf("search query attr = %v, %t, want %q, true", got, ok, "foo.*")
	}
	if got, ok := findLogAttr(lh.Records(), "Artifact found", "path"); !ok || got != "foo.txt" {
		t.Errorf("artifact path attr = %v, %t, want %q, true", got, ok, "foo.txt")
	}
}
