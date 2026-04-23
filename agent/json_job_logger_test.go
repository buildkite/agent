package agent

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/buildkite/agent/v4/api"
	"github.com/google/go-cmp/cmp"
)

func TestJSONJobLogger_JobFields(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer

	job := &api.Job{
		ID: "job-123",
		Env: map[string]string{
			"BUILDKITE_ORGANIZATION_SLUG":     "my-org",
			"BUILDKITE_PIPELINE_SLUG":         "my-pipeline",
			"BUILDKITE_BRANCH":                "main",
			"BUILDKITE_AGENT_META_DATA_QUEUE": "default",
			"BUILDKITE_BUILD_ID":              "build-456",
			"BUILDKITE_BUILD_NUMBER":          "42",
			"BUILDKITE_BUILD_URL":             "https://buildkite.com/my-org/my-pipeline/builds/42",
			"BUILDKITE_STEP_KEY":              "my-step",
		},
	}

	log := NewJSONJobLogger(JobRunnerConfig{AgentStdout: &buf, Job: job})
	log.Write([]byte("hello\n"))

	var got map[string]string
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("failed to parse JSON log: %v", err)
	}

	// Ignore timestamp field from comparison
	delete(got, "ts")

	want := map[string]string{
		"source":       "job",
		"org":          "my-org",
		"pipeline":     "my-pipeline",
		"branch":       "main",
		"queue":        "default",
		"build_id":     "build-456",
		"build_number": "42",
		"build_url":    "https://buildkite.com/my-org/my-pipeline/builds/42",
		"job_url":      "https://buildkite.com/my-org/my-pipeline/builds/42#job-123",
		"job_id":       "job-123",
		"step_key":     "my-step",
		"level":        "INFO",
		"msg":          "hello",
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("Logged JSON diff (-got +want):\n%s", diff)
	}
}

func TestJSONJobLogger_TraceparentFields(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		traceparent string
		wantTraceID string
		wantSpanID  string
	}{
		{
			name:        "valid traceparent",
			traceparent: "00-abcdef1234567890abcdef1234567890-1234567890abcdef-01",
			wantTraceID: "abcdef1234567890abcdef1234567890",
			wantSpanID:  "1234567890abcdef",
		},
		{
			name:        "empty traceparent",
			traceparent: "",
		},
		{
			name:        "malformed traceparent with too few parts",
			traceparent: "00-abcdef1234567890",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer

			job := &api.Job{
				ID:          "test-job",
				Env:         map[string]string{},
				TraceParent: tc.traceparent,
			}

			log := NewJSONJobLogger(JobRunnerConfig{AgentStdout: &buf, Job: job})
			log.Write([]byte("hello\n"))

			var got map[string]string
			if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
				t.Fatalf("failed to parse JSON log: %v", err)
			}

			if got, want := got["trace_id"], tc.wantTraceID; got != want {
				t.Errorf("logged trace_id = %q, want %q", got, want)
			}
			if got, want := got["span_id"], tc.wantSpanID; got != want {
				t.Errorf("logged span_id = %q, want %q", got, want)
			}
		})
	}
}
