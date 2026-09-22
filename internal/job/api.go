package job

import (
	"context"
	"fmt"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/internal/experiments"
	"github.com/buildkite/agent/v4/internal/redact"
	"github.com/buildkite/agent/v4/internal/socket"
	"github.com/buildkite/agent/v4/jobapi"
	"github.com/buildkite/agent/v4/logger"
	"github.com/buildkite/agent/v4/version"
)

// startJobAPI starts the job API server, iff the OS of the box supports it otherwise it returns a
// noop cleanup function. It also sets the BUILDKITE_AGENT_JOB_API_SOCKET and
// BUILDKITE_AGENT_JOB_API_TOKEN environment variables
func (e *Executor) startJobAPI(ctx context.Context) (cleanup func(), err error) {
	cleanup = func() {}
	// Capabilities describe this executor, not an inherited job environment.
	e.shell.Env.Remove("BUILDKITE_AGENT_JOB_API_CAPTURE_ERROR")

	if !socket.Available() {
		e.shell.OptionalWarningf("job-api-unavailable", `The Job API isn't available on this machine, as it's running an unsupported version of Windows.
The Job API is available on Unix agents, and agents running Windows versions after build 17063
We'll continue to run your job, but you won't be able to use the Job API`)
		return cleanup, nil
	}

	socketPath, err := jobapi.NewSocketPath(e.SocketsPath)
	if err != nil {
		return cleanup, fmt.Errorf("creating job API socket path: %w", err)
	}

	jobAPIOpts := []jobapi.ServerOpts{
		jobapi.WithPromiseFailureDeclarer(e.declarePromiseFailure),
	}
	if experiments.IsEnabled(ctx, experiments.CaptureError) {
		reporter := func(requestCtx context.Context, capturedError *jobapi.CapturedError) error {
			// Reporting must not keep a cancelled job alive through its grace period.
			reportCtx, cancel := context.WithCancel(requestCtx)
			defer cancel()

			stop := context.AfterFunc(ctx, cancel)
			defer stop()

			if err := ctx.Err(); err != nil {
				return err
			}
			return e.reportCapturedError(reportCtx, capturedError)
		}

		jobAPIOpts = append(jobAPIOpts, jobapi.WithCapturedErrorReporter(reporter))
	}
	if e.Debug {
		jobAPIOpts = append(jobAPIOpts, jobapi.WithDebug())
	}
	jobAPIOpts = append(jobAPIOpts, jobapi.WithCheckoutOverrideMode(e.CheckoutOverrideMode))
	srv, token, err := jobapi.NewServer(e.shell.Logger, socketPath, e.shell.Env, e.redactors, jobAPIOpts...)
	if err != nil {
		return cleanup, fmt.Errorf("creating job API server: %w", err)
	}
	e.jobAPI = srv

	e.shell.Env.Set("BUILDKITE_AGENT_JOB_API_SOCKET", socketPath)
	e.shell.Env.Set("BUILDKITE_AGENT_JOB_API_TOKEN", token)

	matched, err := redact.MatchAny(e.RedactedVars, "BUILDKITE_AGENT_JOB_API_TOKEN")
	if err != nil {
		e.shell.OptionalWarningf("bad-redacted-vars", "Couldn't match environment variable names against -redacted-vars: %v", err)
	}
	if matched {
		// The Job API token lets the job talk to this executor. When the job ends,
		// the socket should be closed and the token becomes meaningless. Also, the
		// socket should only be accessible to the user running the agent on the
		// local host.
		// So it shouldn't matter if the token is leaked in the logs - in order
		// to make any use of it, someone would have to be on the same host as the
		// same local user at the same time the job is running.
		// However, it looks confusing when an environment variable that looks like
		// an access token with a name ending in _TOKEN is *not* redacted.
		// Conclusion: if the name matches, redact the Job API token.
		// This depends on startJobAPI being called after setupRedactors.
		e.redactors.Add(token)
	}

	if err := srv.Start(); err != nil {
		return cleanup, fmt.Errorf("starting Job API server: %w", err)
	}
	if experiments.IsEnabled(ctx, experiments.CaptureError) {
		e.shell.Env.Set("BUILDKITE_AGENT_JOB_API_CAPTURE_ERROR", "true")
	}

	return func() {
		err = srv.Stop()
		if err != nil {
			e.shell.Errorf("Error stopping Job API server: %v", err)
		}
	}, nil
}

func (e *Executor) reportCapturedError(ctx context.Context, capturedError *jobapi.CapturedError) error {
	apiClient := api.NewClient(logger.Discard, api.Config{
		Endpoint:     e.shell.Env.GetString("BUILDKITE_AGENT_ENDPOINT", ""),
		Token:        e.shell.Env.GetString("BUILDKITE_AGENT_ACCESS_TOKEN", ""),
		DisableHTTP2: e.shell.Env.GetBool("BUILDKITE_NO_HTTP2", false),
		UserAgent:    version.UserAgent(),
	})

	_, err := apiClient.CaptureJobError(ctx, e.JobID, &api.JobCapturedError{
		Code:           capturedError.Code,
		Message:        capturedError.Message,
		Timestamp:      *capturedError.Timestamp,
		IdempotencyKey: capturedError.IdempotencyKey,
		Context:        capturedError.Context,
	})
	return err
}

// declarePromiseFailure declares a promised failure for the current job to the
// Buildkite API. The Job API server debounces calls, so this runs at most once
// per successfully-declared exit status. It returns the status code of the most
// recent API response (0 if none was received, e.g. a network error) and an
// error describing any failure.
func (e *Executor) declarePromiseFailure(ctx context.Context, exitStatus int, reason string) (int, error) {
	// logger.Discard keeps the access token (in HTTP debug dumps) out of the job
	// log; retry warnings still go to the shell logger.
	apiClient := api.NewClient(logger.Discard, api.Config{
		Endpoint:     e.shell.Env.GetString("BUILDKITE_AGENT_ENDPOINT", ""),
		Token:        e.shell.Env.GetString("BUILDKITE_AGENT_ACCESS_TOKEN", ""),
		DisableHTTP2: e.shell.Env.GetBool("BUILDKITE_NO_HTTP2", false),
		UserAgent:    version.UserAgent(),
	})

	req := &api.JobPromiseFailureRequest{
		ExitStatus: exitStatus,
		Reason:     reason,
	}

	return apiClient.PromiseFailureWithRetry(ctx, e.JobID, req, e.shell.Warningf)
}
