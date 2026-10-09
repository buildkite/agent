package agent

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/buildkite/agent/v4/internal/process"
)

func TestRunJob_ExecutionErrorSignalReason(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		cancelled     bool
		agentStopping bool
		want          string
	}{
		{name: "not_cancelled", want: SignalReasonProcessRunError},
		{name: "cancelled", cancelled: true, want: SignalReasonCancel},
		{name: "agent_stopping", cancelled: true, agentStopping: true, want: SignalReasonAgentStop},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := &JobRunner{process: failingExecution{}, jobLogs: io.Discard}
			r.cancelled.Store(tc.cancelled)
			r.agentStopping.Store(tc.agentStopping)

			exit := r.runJob(t.Context())
			if exit.Status != -1 {
				t.Errorf("exit.Status = %d, want -1", exit.Status)
			}
			if exit.SignalReason != tc.want {
				t.Errorf("exit.SignalReason = %q, want %q", exit.SignalReason, tc.want)
			}
		})
	}
}

// failingExecution is a JobExecution whose Run fails, as an executor's does
// when the job is cancelled before its container starts.
type failingExecution struct{}

func (failingExecution) Started() <-chan struct{}       { return nil }
func (failingExecution) Done() <-chan struct{}          { return nil }
func (failingExecution) Run(context.Context) error      { return errors.New("cancelled before start") }
func (failingExecution) Interrupt() error               { return nil }
func (failingExecution) Terminate() error               { return nil }
func (failingExecution) WaitStatus() process.WaitStatus { return nil }
func (failingExecution) Cleanup(context.Context) error  { return nil }
