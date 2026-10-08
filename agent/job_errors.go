package agent

import (
	"context"
	"crypto/rand"
	"time"

	"github.com/buildkite/agent/v4/api"
)

// captureJobError reports a failure the agent observed outside the job's
// bootstrap process, such as refusing to run the job, when the job opted in
// with BUILDKITE_CAPTURE_AGENT_ERRORS. There is no Local Job API here, so the
// runner reports directly with the job's token. Messages are written by the
// agent, contain no job output to redact, and mask URL credentials. Delivery is
// best-effort and never changes the job's outcome.
func (r *JobRunner) captureJobError(ctx context.Context, code, message string) {
	if r.conf.Job.Env["BUILDKITE_CAPTURE_AGENT_ERRORS"] != "true" {
		return
	}
	// Report even while the agent is stopping, but never wait long.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	_, err := r.apiClient.CaptureJobError(ctx, r.conf.Job.ID, &api.JobCapturedError{
		Code:           code,
		Message:        message,
		Timestamp:      time.Now(),
		IdempotencyKey: rand.Text(),
	})
	if err != nil {
		r.agentLogger.Warnf("Could not capture job error %q: %v", code, err)
	}
}
