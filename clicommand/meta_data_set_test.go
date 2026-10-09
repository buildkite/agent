package clicommand

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/buildkite/agent/v4/api"
)

// Regression test for https://linear.app/buildkite/issue/A-1899. urfave/cli
// v3.11.0 trimmed leading/trailing whitespace from positional arguments
// (fixed upstream in urfave/cli#2423, released in v3.12.0), so
// `meta-data set key $'\n* item\n'` would post a value of "* item". The
// value must reach the Agent API byte for byte.
func TestMetaDataSetCommandPreservesValueWhitespace(t *testing.T) {
	var mu sync.Mutex
	var posted api.MetaData
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost || req.URL.RequestURI() != "/jobs/jobid/data/set" {
			t.Errorf("unexpected HTTP request: %s %v", req.Method, req.URL.RequestURI())
		}
		var metaData api.MetaData
		if err := json.NewDecoder(req.Body).Decode(&metaData); err != nil {
			t.Errorf("decoding posted meta-data: %v", err)
		}
		mu.Lock()
		posted = metaData
		mu.Unlock()
		_, _ = rw.Write([]byte(`{}`))
	}))
	defer server.Close()

	args := []string{
		"set",
		"key",
		"\n* item one\n",
		"--agent-access-token", "agentaccesstoken",
		"--endpoint", server.URL,
		"--job", "jobid",
	}

	// Run a copy of the command: urfave/cli mutates the command on Run
	// (adding a generated help subcommand/flag), which would pollute the
	// shared MetaDataSetCommand that the command registry and structural
	// tests use.
	cmd := *MetaDataSetCommand
	if err := cmd.Run(t.Context(), args); err != nil {
		t.Fatalf("MetaDataSetCommand.Run(%q) error = %v", args, err)
	}

	mu.Lock()
	defer mu.Unlock()
	if posted.Value != "\n* item one\n" {
		t.Errorf("posted meta-data value = %q, want %q", posted.Value, "\n* item one\n")
	}
	if posted.Key != "key" {
		t.Errorf("posted meta-data key = %q, want %q", posted.Key, "key")
	}
}

// The command must still reject whitespace-only values, which carry no data.
func TestMetaDataSetCommandRejectsWhitespaceOnlyValue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		t.Errorf("unexpected HTTP request: %s %v", req.Method, req.URL.RequestURI())
	}))
	defer server.Close()

	args := []string{
		"set",
		"key",
		"  \n\t  ",
		"--agent-access-token", "agentaccesstoken",
		"--endpoint", server.URL,
		"--job", "jobid",
	}

	// Run a copy of the command, see TestMetaDataSetCommandPreservesValueWhitespace.
	cmd := *MetaDataSetCommand
	err := cmd.Run(t.Context(), args)
	if err == nil {
		t.Errorf("MetaDataSetCommand.Run(%q) = nil error, want whitespace-only value error", args)
	}
}
