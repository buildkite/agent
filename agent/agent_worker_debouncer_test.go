package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/buildkite/agent/v4/logger"
)

func TestDebouncer_StreamUnhealthyDuringAction_ReleasesBaton(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		actionErr error
	}{
		{
			name:      "action succeeds",
			actionErr: nil,
		},
		{
			name:      "action errors",
			actionErr: errors.New("action failed"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()

			worker := &AgentWorker{
				logger: logger.NewBuffer(),
				stop:   make(chan struct{}),
			}

			bat := newBaton()
			<-bat.Acquire()
			bat.Acquired(actorDebouncer)

			inCh := make(chan actionMessage)
			outCh := make(chan actionMessage)

			go func() {
				_ = worker.runDebouncer(ctx, bat, outCh, inCh)
			}()

			inCh <- actionMessage{jobID: "job-1"}

			var dispatched actionMessage
			select {
			case dispatched = <-outCh:
				if dispatched.jobID != "job-1" {
					t.Fatalf("unexpected jobID: got %q, want %q", dispatched.jobID, "job-1")
				}
			case <-ctx.Done():
				t.Fatal("timed out waiting for action to be dispatched")
			}

			inCh <- actionMessage{unhealthy: true}

			time.Sleep(50 * time.Millisecond)

			if !bat.HeldBy(actorDebouncer) {
				t.Fatal("baton should still be held while action is in progress")
			}

			dispatched.errCh <- tc.actionErr

			select {
			case <-bat.Acquire():
				bat.Acquired(actorPingLoop)
				bat.Release(actorPingLoop)
			case <-time.After(500 * time.Millisecond):
				t.Fatal("debouncer failed to release baton after action completed with unhealthy stream")
			}
		})
	}
}
