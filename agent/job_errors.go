package agent

import (
	"context"
	"crypto/rand"
	"time"
	"unicode/utf8"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/internal/redact"
	"github.com/buildkite/agent/v4/jobapi"
)

// captureJobError reports a failure the agent observed outside the job's
// bootstrap process, such as refusing to run the job, when the job opted in
// with BUILDKITE_CAPTURE_AGENT_ERRORS. There is no Local Job API here, so the
// runner reports directly with the job's token, and does what the Local Job
// API would: it masks URL credentials and query strings, then shortens the
// message to the agent's budget. Messages are written by the agent and contain
// no job output to redact. Delivery is best-effort and never changes the job's
// outcome.
func (r *JobRunner) captureJobError(ctx context.Context, code, message string) {
	r.reportJobError(ctx, code, maskURLs(message))
}

// maskURLs masks URL credentials and query strings, as the Local Job API does.
func maskURLs(text string) string {
	return redact.URLQueriesInText(redact.URLCredentialsInText(text))
}

// reportJobError is captureJobError for a message whose URLs are already
// masked where needed.
func (r *JobRunner) reportJobError(ctx context.Context, code, message string) {
	if r.conf.Job.Env["BUILDKITE_CAPTURE_AGENT_ERRORS"] != "true" {
		return
	}
	if runes := []rune(message); len(runes) > jobapi.MaxCapturedErrorDetail {
		const marker = "…[truncated]"
		message = string(runes[:jobapi.MaxCapturedErrorDetail-utf8.RuneCountInString(marker)]) + marker
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
