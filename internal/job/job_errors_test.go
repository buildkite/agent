package job

import (
	"context"
	"net/http"
	"testing"

	"github.com/buildkite/agent/v4/api"
)

// agentErrorCaptureServer returns an executor whose Local Job API forwards
// captured errors to reports, with agent error capture enabled.
func agentErrorCaptureServer(t *testing.T) (context.Context, *Executor, <-chan api.JobCapturedError) {
	t.Helper()
	ctx, e, reports := gitErrorCaptureServer(t, false, http.StatusCreated)
	e.shell.Env.Set("BUILDKITE_CAPTURE_AGENT_ERRORS", "true")
	return ctx, e, reports
}

func TestCaptureJobErrorOptIn(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, optIn, capability string
		want                    bool
	}{
		{"unset", "", "true", false},
		{"disabled", "false", "true", false},
		{"invalid", "yes", "true", false},
		{"enabled", "true", "true", true},
		{"API unavailable", "true", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, e, reports := agentErrorCaptureServer(t)
			e.shell.Env.Set("BUILDKITE_CAPTURE_AGENT_ERRORS", tc.optIn)
			e.shell.Env.Set("BUILDKITE_AGENT_JOB_API_CAPTURE_ERROR", tc.capability)

			captureJobError(ctx, e.shell, "hook_failed", "The hook failed.")

			if got := len(reports) == 1; got != tc.want {
				t.Fatalf("captured = %t, want %t", got, tc.want)
			}
			if tc.want {
				report := <-reports
				if report.Code != "hook_failed" || report.Message != "The hook failed." {
					t.Errorf("report = %+v", report)
				}
			}
		})
	}
}

func TestCaptureJobErrorSkipsCancelledJobs(t *testing.T) {
	t.Parallel()
	ctx, e, reports := agentErrorCaptureServer(t)
	ctx, cancel := context.WithCancel(ctx)
	cancel()

	captureJobError(ctx, e.shell, "hook_failed", "The hook failed.")

	if len(reports) != 0 {
		t.Fatalf("reports = %d, want none after cancellation", len(reports))
	}
}

func TestCaptureJobErrorSkipsJobsMarkedCancelled(t *testing.T) {
	t.Parallel()
	// Hooks such as post-command keep running during the grace period after
	// the executor is cancelled, which marks the job as cancelled.
	ctx, e, reports := agentErrorCaptureServer(t)
	e.shell.Env.Set("BUILDKITE_JOB_CANCELLED", "true")

	captureJobError(ctx, e.shell, "hook_failed", "The hook failed.")

	if len(reports) != 0 {
		t.Fatalf("reports = %d, want none for a cancelled job", len(reports))
	}
}
