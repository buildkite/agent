package job

import (
	"fmt"

	"github.com/buildkite/agent/v3/experiments"
	"github.com/buildkite/agent/v3/jobapi"
)

// startJobAPI starts the job API server, iff the job API experiment is enabled, and the OS of the box supports it
// otherwise it returns a noop cleanup function
// It also sets the BUILDKITE_AGENT_JOB_API_SOCKET and BUILDKITE_AGENT_JOB_API_TOKEN environment variables
func (b *Executor) startJobAPI() (cleanup func(), err error) {
	cleanup = func() {}

	if !experiments.IsEnabled(experiments.JobAPI) {
		return cleanup, nil
	}

	if !jobapi.Available() {
		b.shell.Warningf("The Job API isn't available on this machine, as it's running an unsupported version of Windows")
		b.shell.Warningf("The Job API is available on Unix agents, and agents running Windows versions after build 17063")
		b.shell.Warningf("We'll continue to run your job, but you won't be able to use the Job API")
		return cleanup, nil
	}

	socketPath, err := jobapi.NewSocketPath(b.Config.SocketsPath)
	if err != nil {
		return cleanup, fmt.Errorf("creating job API socket path: %v", err)
	}

	srv, token, err := jobapi.NewServer(b.shell.Logger, socketPath, b.shell.Env)
	if err != nil {
		return cleanup, fmt.Errorf("creating job API server: %v", err)
	}

	b.shell.Env.Set("BUILDKITE_AGENT_JOB_API_SOCKET", socketPath)
	b.shell.Env.Set("BUILDKITE_AGENT_JOB_API_TOKEN", token)

	if err := srv.Start(); err != nil {
		return cleanup, fmt.Errorf("starting Job API server: %v", err)
	}

	return func() {
		err = srv.Stop()
		if err != nil {
			b.shell.Errorf("Error stopping Job API server: %v", err)
		}
	}, nil
}
