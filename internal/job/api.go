package job

import (
	"fmt"

	"github.com/buildkite/agent/v3/internal/redact"
	"github.com/buildkite/agent/v3/internal/socket"
	"github.com/buildkite/agent/v3/jobapi"
)

// startJobAPI starts the job API server, iff the OS of the box supports it otherwise it returns a
// noop cleanup function. It also sets the BUILDKITE_AGENT_JOB_API_SOCKET and
// BUILDKITE_AGENT_JOB_API_TOKEN environment variables
func (e *Executor) startJobAPI() (cleanup func(), err error) {
	cleanup = func() {}

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

	jobAPIOpts := []jobapi.ServerOpts{}
	if e.Debug {
		jobAPIOpts = append(jobAPIOpts, jobapi.WithDebug())
	}
	srv, token, err := jobapi.NewServer(e.shell.Logger, socketPath, e.shell.Env, e.redactors, jobAPIOpts...)
	if err != nil {
		return cleanup, fmt.Errorf("creating job API server: %w", err)
	}

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

	return func() {
		err = srv.Stop()
		if err != nil {
			e.shell.Errorf("Error stopping Job API server: %v", err)
		}
	}, nil
}
