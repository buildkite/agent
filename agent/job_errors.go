package agent

import (
	"context"
	"crypto/rand"
	"io"
	"net/http"
	"os"
	"time"
	"unicode/utf8"

	"github.com/buildkite/agent/v4/api"
	envutil "github.com/buildkite/agent/v4/env"
	"github.com/buildkite/agent/v4/internal/experiments"
	"github.com/buildkite/agent/v4/internal/joberrorosc"
	"github.com/buildkite/agent/v4/internal/redact"
	"github.com/buildkite/agent/v4/jobapi"
)

// captureJobError reports a failure the agent observed outside the job's
// bootstrap process, such as refusing to run the job, when the job opted in
// with BUILDKITE_CAPTURE_AGENT_ERRORS. There is no Local Job API here, so the
// runner does what the Local Job API would: it masks URL credentials and query
// strings, then shortens the message to the agent's budget. Messages are written
// by the agent and contain no job output to redact. Delivery is through OSC when
// enabled, otherwise directly with the job's token. It is best-effort and never
// changes the job's outcome.
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
	if !r.agentErrorCaptureEnabled() {
		return
	}
	if runes := []rune(message); len(runes) > jobapi.MaxCapturedErrorDetail {
		const marker = "…[truncated]"
		message = string(runes[:jobapi.MaxCapturedErrorDetail-utf8.RuneCountInString(marker)]) + marker
	}
	report := api.JobCapturedError{
		Code:           code,
		Message:        message,
		Timestamp:      time.Now(),
		IdempotencyKey: rand.Text(),
	}
	if experiments.IsEnabled(ctx, experiments.JobErrorsOSC) {
		if err := joberrorosc.Write(r.jobLogs, report); err != nil {
			r.agentLogger.Warnf("Could not write job error %q to the log", code)
		}
		return
	}
	_ = r.sendCapturedError(ctx, report)
}

func (r *JobRunner) sendCapturedError(ctx context.Context, report api.JobCapturedError) error {
	// Report even while the agent is stopping, but never wait long. Preserve
	// the producer's timestamp and key rather than assigning them at extraction.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	_, err := r.apiClient.CaptureJobError(ctx, r.conf.Job.ID, &report)
	if err != nil {
		r.agentLogger.Warnf("Could not capture job error %q: %v", report.Code, err)
	}
	return err
}

// capturedErrorLogWriter extracts errors before timestamping or log size
// limits can change/drop the stream. Keep failures best-effort, and stop API
// calls after the same permanent refusals as the Local Job API.
func (r *JobRunner) capturedErrorLogWriter(ctx context.Context, out io.Writer) io.Writer {
	refused := false // callbacks are serialized by the OSC writer
	return joberrorosc.NewWriter(out, func(report api.JobCapturedError) {
		if refused {
			return
		}
		err := r.sendCapturedError(ctx, report)
		if api.IsErrHavingStatus(err, http.StatusNotFound) || api.IsErrHavingStatus(err, http.StatusUnprocessableEntity) {
			refused = true
		}
	})
}

// agentErrorCaptureEnabled reads the BUILDKITE_CAPTURE_AGENT_ERRORS opt-in as
// the job's bootstrap would see it: from the job's env, or else from the
// agent's own environment, such as when the agent's operator set it or
// protected the name from jobs, which makes createEnvironment drop the job's
// value.
func (r *JobRunner) agentErrorCaptureEnabled() bool {
	const name = "BUILDKITE_CAPTURE_AGENT_ERRORS"
	optIn, ok := r.conf.Job.Env[name]
	agentConf := r.conf.AgentConfiguration
	if patterns, err := envutil.ProtectedEnvPatterns(agentConf.ProtectedEnvironmentVariables, agentConf.ProtectDangerousGitEnvironmentVariables); err == nil && envutil.MatchesProtectedEnv(name, patterns) {
		ok = false
	}
	if !ok {
		optIn = os.Getenv(name)
	}
	return optIn == "true"
}
