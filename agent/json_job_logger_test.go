package agent

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/buildkite/agent/v3/api"
)

func TestNewJsonJobLoggerJobFields(t *testing.T) {
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

	log := NewJsonJobLogger(JobRunnerConfig{AgentStdout: &buf, Job: job})
	log.Write([]byte("hello\n"))

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("failed to parse JSON log: %v", err)
	}

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
	}
	for field, wantVal := range want {
		if got, ok := entry[field].(string); !ok || got != wantVal {
			t.Errorf("%s = %q, want %q", field, got, wantVal)
		}
	}
}

func TestNewJsonJobLoggerTraceparentFields(t *testing.T) {
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
			var buf bytes.Buffer

			job := &api.Job{
				ID:          "test-job",
				Env:         map[string]string{},
				TraceParent: tc.traceparent,
			}

			log := NewJsonJobLogger(JobRunnerConfig{AgentStdout: &buf, Job: job})
			log.Write([]byte("hello\n"))

			var entry map[string]any
			if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
				t.Fatalf("failed to parse JSON log: %v", err)
			}

			if tc.wantTraceID != "" {
				if got, ok := entry["trace_id"].(string); !ok || got != tc.wantTraceID {
					t.Errorf("trace_id = %q, want %q", got, tc.wantTraceID)
				}
			} else {
				if _, ok := entry["trace_id"]; ok {
					t.Error("unexpected trace_id field in log output")
				}
			}
			if tc.wantSpanID != "" {
				if got, ok := entry["span_id"].(string); !ok || got != tc.wantSpanID {
					t.Errorf("span_id = %q, want %q", got, tc.wantSpanID)
				}
			} else {
				if _, ok := entry["span_id"]; ok {
					t.Error("unexpected span_id field in log output")
				}
			}
		})
	}
}
