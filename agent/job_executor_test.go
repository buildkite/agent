package agent

import (
	"fmt"
	"testing"

	"github.com/buildkite/agent/v4/internal/dockerexec"
	"github.com/buildkite/agent/v4/logger"
)

func TestNewJobExecutor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		executor string
		want     JobExecutor
	}{
		{executor: "", want: &execExecutor{}},
		{executor: ExecutorExec, want: &execExecutor{}},
		{executor: ExecutorKubernetes, want: &kubernetesExecutor{}},
	}
	for _, tc := range tests {
		got, err := newJobExecutor(logger.Discard, JobRunnerConfig{Executor: tc.executor})
		if err != nil {
			t.Errorf("newJobExecutor(Executor: %q) error = %v", tc.executor, err)
			continue
		}
		if gotT, wantT := fmt.Sprintf("%T", got), fmt.Sprintf("%T", tc.want); gotT != wantT {
			t.Errorf("newJobExecutor(Executor: %q) = %s, want %s", tc.executor, gotT, wantT)
		}
	}

	// The docker executor needs the executor prepared at agent start.
	if got, err := newJobExecutor(logger.Discard, JobRunnerConfig{Executor: ExecutorDocker}); err == nil {
		t.Errorf("newJobExecutor(Executor: docker, unprepared) = %T, want an error", got)
	}
	prepared := JobRunnerConfig{Executor: ExecutorDocker, AgentConfiguration: AgentConfiguration{DockerExecutor: &dockerexec.Executor{}}}
	if got, err := newJobExecutor(logger.Discard, prepared); err != nil {
		t.Errorf("newJobExecutor(Executor: docker) error = %v", err)
	} else if _, ok := got.(dockerExecutor); !ok {
		t.Errorf("newJobExecutor(Executor: docker) = %T, want dockerExecutor", got)
	}

	// An unknown name must fail rather than fall back to running bootstrap on
	// the host.
	if got, err := newJobExecutor(logger.Discard, JobRunnerConfig{Executor: "dokcer"}); err == nil {
		t.Errorf("newJobExecutor(Executor: %q) = %T, want an error", "dokcer", got)
	}
}
