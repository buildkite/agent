package agent

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/buildkite/agent/v4/kubernetes"
	"github.com/buildkite/agent/v4/logger"
)

// kubernetesExecutor serves the job to bootstrap containers elsewhere in the
// pod, which connect over a socket in the job context directory.
type kubernetesExecutor struct {
	logger                logger.Logger
	containerStartTimeout time.Duration
}

func (e *kubernetesExecutor) New(_ context.Context, req JobExecutionRequest) (JobExecution, error) {
	// Thank you Mario, but our bootstrap is in another container
	containerCount, err := strconv.Atoi(os.Getenv("BUILDKITE_CONTAINER_COUNT"))
	if err != nil {
		return nil, fmt.Errorf("failed to parse BUILDKITE_CONTAINER_COUNT: %w", err)
	}

	return kubernetesExecution{kubernetes.NewRunner(e.logger, kubernetes.RunnerConfig{
		SocketPath:         kubernetes.SocketPath(req.ContextDir),
		Stdout:             req.Output,
		Stderr:             req.Output,
		ClientCount:        containerCount,
		Env:                append(os.Environ(), req.Env...),
		ClientStartTimeout: e.containerStartTimeout,
		ClientLostTimeout:  30 * time.Second,
	})}, nil
}

// kubernetesExecution is one job served to the pod's containers. The pod is
// managed outside the agent, so there is nothing to clean up.
type kubernetesExecution struct {
	*kubernetes.Runner
}

func (kubernetesExecution) Cleanup(context.Context) error { return nil }
