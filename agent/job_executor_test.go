package agent

import (
	"fmt"
	"testing"

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

	// An unknown name must fail rather than fall back to running bootstrap on
	// the host.
	if got, err := newJobExecutor(logger.Discard, JobRunnerConfig{Executor: "dokcer"}); err == nil {
		t.Errorf("newJobExecutor(Executor: %q) = %T, want an error", "dokcer", got)
	}
}
