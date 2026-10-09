package clicommand

import (
	"context"
	"testing"

	"github.com/buildkite/agent/v4/jobapi"
	"github.com/buildkite/agent/v4/logger"
)

// startAgentErrorTestServer starts a Local Job API that records captured
// errors, and opts the job in to agent error capture.
func startAgentErrorTestServer(t *testing.T) *[]jobapi.CapturedError {
	t.Helper()
	var reports []jobapi.CapturedError
	startCaptureErrorTestServer(t, func(_ context.Context, report *jobapi.CapturedError) error {
		reports = append(reports, *report)
		return nil
	})
	t.Setenv("BUILDKITE_CAPTURE_AGENT_ERRORS", "true")
	t.Setenv("BUILDKITE_AGENT_JOB_API_CAPTURE_ERROR", "true")
	return &reports
}

func TestCaptureAgentErrorOptIn(t *testing.T) {
	for _, test := range []struct {
		name, optIn, capability string
		want                    bool
	}{
		{"unset", "", "true", false},
		{"disabled", "false", "true", false},
		{"enabled", "true", "true", true},
		{"API unavailable", "true", "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			reports := startAgentErrorTestServer(t)
			t.Setenv("BUILDKITE_CAPTURE_AGENT_ERRORS", test.optIn)
			t.Setenv("BUILDKITE_AGENT_JOB_API_CAPTURE_ERROR", test.capability)

			captureAgentError(t.Context(), logger.Discard, "artifact_upload_failed", "Failed to upload artifacts.")

			if got := len(*reports) == 1; got != test.want {
				t.Fatalf("captured = %t, want %t", got, test.want)
			}
			if test.want {
				report := (*reports)[0]
				if report.Code != "artifact_upload_failed" || report.Message != "Failed to upload artifacts." {
					t.Errorf("report = %+v", report)
				}
			}
		})
	}
}

func TestCaptureAgentErrorSkipsCancelledCommands(t *testing.T) {
	reports := startAgentErrorTestServer(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	captureAgentError(ctx, logger.Discard, "artifact_upload_failed", "Failed to upload artifacts.")

	if len(*reports) != 0 {
		t.Fatalf("reports = %d, want none after cancellation", len(*reports))
	}
}
