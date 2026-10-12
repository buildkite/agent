package agent

import (
	"context"

	"github.com/buildkite/agent/v4/internal/dockerexec"
)

// dockerExecutor runs each job's bootstrap in a container. The
// dockerexec.Executor it wraps was prepared once at agent start.
type dockerExecutor struct {
	*dockerexec.Executor
}

func (e dockerExecutor) New(_ context.Context, req JobExecutionRequest) (JobExecution, error) {
	return e.NewExecution(dockerexec.Request{
		JobID:         req.JobID,
		Env:           req.Env,
		Output:        req.Output,
		JobLogTmpfile: req.JobLogTmpfile,
	}), nil
}
