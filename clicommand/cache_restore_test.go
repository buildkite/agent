package clicommand

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/urfave/cli/v3"
)

func TestCacheRestoreFailureIsCaptured(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("cache.yml", []byte("caches:\n  - name: rubocop\n    cache_key: [rubocop-v1]\n    target_paths: [rubocop-cache]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"message":"Cache restore is not permitted"}`)
	}))
	t.Cleanup(server.Close)
	reports := startAgentErrorTestServer(t)

	cmd := *CacheRestoreCommand
	app := &cli.Command{Commands: []*cli.Command{&cmd}}
	args := []string{"buildkite-agent", "restore", "--endpoint", server.URL, "--agent-access-token", "test-token", "--registry", "test", "--cache-config-file", "cache.yml", "--name", "rubocop", "--cache-fail-on-error"}
	if err := app.Run(t.Context(), args); err == nil {
		t.Fatal("cache restore succeeded, want failure")
	}

	if len(*reports) != 1 {
		t.Fatalf("reports = %+v, want one", *reports)
	}
	report := (*reports)[0]
	if report.Code != "cache_restore_failed" || report.Message != `Failed to restore caches [rubocop] from registry "test": failed to restore cache "rubocop": failed to retrieve cache: request failed with status: 403 Forbidden` {
		t.Errorf("report = %q %q", report.Code, report.Message)
	}
}
