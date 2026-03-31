package agent

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/buildkite/agent/v4/api"
)

func TestJobLoggerJSONFormat(t *testing.T) {
	var buf bytes.Buffer
	conf := JobRunnerConfig{
		AgentStdout:        &buf,
		AgentConfiguration: AgentConfiguration{LogFormat: "json"},
		Job: &api.Job{
			ID:  "test-job",
			Env: map[string]string{},
		},
	}

	log := NewJobLogger(conf)
	if _, err := log.Write([]byte("hello\n")); err != nil {
		t.Fatalf("log.Write() = %v", err)
	}

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("expected valid JSON output, got %q: %v", buf.String(), err)
	}
	if got, ok := entry["msg"].(string); !ok || got != "hello" {
		t.Errorf("msg = %q, want %q", got, "hello")
	}
}

func TestJobLoggerTextFormat(t *testing.T) {
	var buf bytes.Buffer
	conf := JobRunnerConfig{
		AgentStdout:        &buf,
		AgentConfiguration: AgentConfiguration{LogFormat: "text"},
		Job: &api.Job{
			ID:  "test-job",
			Env: map[string]string{},
		},
	}

	log := NewJobLogger(conf)
	if _, err := log.Write([]byte("hello\n")); err != nil {
		t.Fatalf("log.Write() = %v", err)
	}

	if err := json.Unmarshal(buf.Bytes(), &map[string]any{}); err == nil {
		t.Errorf("expected non-JSON output for text format, but got valid JSON")
	}
	if got := buf.String(); got != "hello\n" {
		t.Errorf("output = %q, want %q", got, "hello\n")
	}
}
