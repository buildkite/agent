package clicommand

import (
	"context"
	"errors"
	"fmt"
	"strings"
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

func TestUploadErrorText(t *testing.T) {
	t.Parallel()
	joined := fmt.Errorf("uploading artifacts: %w", errors.Join(
		errors.New("a.txt: PUT https://bucket.example/a.txt?sig=one: 403 Forbidden"),
		errors.New("b.txt: PUT https://bucket.example/b.txt?sig=two: 403 Forbidden"),
		errors.New("c.txt: PUT https://bucket.example/c.txt?sig=three: 403 Forbidden"),
	))
	for _, test := range []struct {
		name string
		err  error
		want string
	}{
		{"one error", errors.New("GET https://bucket.example/a.txt?sig=one: 404 Not Found"), "GET https://bucket.example/a.txt?[REDACTED]: 404 Not Found"},
		{"errors for several files", joined, "a.txt: PUT https://bucket.example/a.txt?[REDACTED]: 403 Forbidden (and 2 more errors)"},
		{"errors for two files", errors.Join(errors.New("a.txt: denied"), errors.New("b.txt: denied")), "a.txt: denied (and 1 more error)"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := uploadErrorText(test.err); got != test.want {
				t.Errorf("uploadErrorText() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestStorageErrorTextKeepsJoinedCauses(t *testing.T) {
	t.Parallel()
	// Cache restore joins a sentinel with the actual cause, which must survive.
	err := errors.Join(errors.New("cache restore failed after modifying target paths"), errors.New(`restoring "node_modules": directory not empty`))
	if got := storageErrorText(err); !strings.Contains(got, "directory not empty") {
		t.Errorf("storageErrorText() = %q, want both causes", got)
	}
}
