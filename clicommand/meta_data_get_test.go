package clicommand

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestMetaDataGetCommandMissingKeyWithDefault(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/jobs/jobid/data/get" {
			t.Errorf("unexpected HTTP request: %s %v", r.Method, r.URL)
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	stderr, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	originalStderr := os.Stderr
	os.Stderr = stderr
	t.Cleanup(func() {
		os.Stderr = originalStderr
		_ = stderr.Close()
	})

	var stdout bytes.Buffer
	cmd := *MetaDataGetCommand
	cmd.Writer = &stdout
	args := []string{
		"get", "key", "--default", "fallback",
		"--agent-access-token", "unused-token", "--endpoint", server.URL, "--job", "jobid",
	}
	if err := cmd.Run(t.Context(), args); err != nil {
		t.Fatalf("MetaDataGetCommand.Run(%q) error = %v", args, err)
	}
	if got, want := stdout.String(), "fallback\n"; got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
	logs, err := os.ReadFile(stderr.Name())
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 0 {
		t.Errorf("stderr = %q, want no logs for an expected fallback", logs)
	}
}
