package job

import (
	"fmt"

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

	socketPath, err := jobapi.NewSocketPath(e.ExecutorConfig.SocketsPath)
	if err != nil {
		return cleanup, fmt.Errorf("creating job API socket path: %v", err)
	}

	jobAPIOpts := []jobapi.ServerOpts{}
	if e.ExecutorConfig.Debug {
		jobAPIOpts = append(jobAPIOpts, jobapi.WithDebug())
	}
	srv, token, err := jobapi.NewServer(e.shell.Logger, socketPath, e.shell.Env, jobAPIOpts...)
	if err != nil {
		return cleanup, fmt.Errorf("creating job API server: %v", err)
	}

	e.shell.Env.Set("BUILDKITE_AGENT_JOB_API_SOCKET", socketPath)
	e.shell.Env.Set("BUILDKITE_AGENT_JOB_API_TOKEN", token)

	if err := srv.Start(); err != nil {
		return cleanup, fmt.Errorf("starting Job API server: %v", err)
	}

	return func() {
		err = srv.Stop()
		if err != nil {
			e.shell.Errorf("Error stopping Job API server: %v", err)
		}
	}, nil
}
