package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/buildkite/agent/v4/internal/process"
	"github.com/buildkite/agent/v4/logger"
)

// JobExecutor chooses where and how the bootstrap for a job runs. It holds
// configuration that is the same for every job the agent runs.
type JobExecutor interface {
	// New prepares one job execution. It must not create anything that needs
	// cleaning up, because Cleanup is only guaranteed once Run has been
	// called.
	New(ctx context.Context, req JobExecutionRequest) (JobExecution, error)
}

// JobExecution is one running job. Interrupt and Terminate can be called at
// any time and from any goroutine: before Run, while Run is in progress, or
// after Cleanup.
type JobExecution interface {
	// Started is closed once the execution has begun launching the job.
	Started() <-chan struct{}

	// Done is closed once the job has finished.
	Done() <-chan struct{}

	// Run launches the job and blocks until it finishes. An error means the
	// executor failed and there is no trustworthy job result.
	Run(ctx context.Context) error

	// Interrupt asks the job to stop gracefully.
	Interrupt() error

	// Terminate stops the job forcefully.
	Terminate() error

	// WaitStatus reports how the job exited. It is only meaningful after Run
	// returns nil.
	WaitStatus() process.WaitStatus

	// Cleanup releases anything the execution created. If Run was called,
	// JobRunner calls Cleanup once after Run returns. It may also call
	// Cleanup for an execution whose Run was never called.
	Cleanup(ctx context.Context) error
}

// JobExecutionRequest holds the per-job values an executor needs.
type JobExecutionRequest struct {
	// JobID identifies the job.
	JobID string

	// Env is the job environment from createEnvironment. It does not include
	// the agent's own process environment.
	Env []string

	// ContextDir is the job context directory, which holds the job env files
	// and the job timeout marker.
	ContextDir string

	// Output receives the job's stdout and stderr.
	Output io.Writer

	// JobLogTmpfile is the path of the job log tmpfile, or empty if
	// enable-job-log-tmpfile is off.
	JobLogTmpfile string
}

// Values for the executor setting.
const (
	ExecutorExec       = "exec"
	ExecutorKubernetes = "kubernetes"
	ExecutorDocker     = "docker"
)

// newJobExecutor returns the executor named by conf.Executor. An empty name
// means ExecutorExec.
func newJobExecutor(l logger.Logger, conf JobRunnerConfig) (JobExecutor, error) {
	switch conf.Executor {
	case "", ExecutorExec:
		return &execExecutor{
			logger:            l,
			bootstrapScript:   conf.AgentConfiguration.BootstrapScript,
			buildPath:         conf.AgentConfiguration.BuildPath,
			runInPty:          conf.AgentConfiguration.RunInPty,
			cancelSignal:      conf.CancelSignal,
			signalGracePeriod: conf.AgentConfiguration.CancelSignalTimeout,
		}, nil
	case ExecutorKubernetes:
		return &kubernetesExecutor{
			logger:                l,
			containerStartTimeout: conf.KubernetesContainerStartTimeout,
		}, nil
	case ExecutorDocker:
		if conf.AgentConfiguration.DockerExecutor == nil {
			return nil, errors.New("the docker executor was not prepared at agent start")
		}
		return dockerExecutor{conf.AgentConfiguration.DockerExecutor}, nil
	default:
		return nil, fmt.Errorf("unknown executor %q", conf.Executor)
	}
}

// jobExecutionCleanupTimeout bounds JobExecution.Cleanup.
const jobExecutionCleanupTimeout = 30 * time.Second
