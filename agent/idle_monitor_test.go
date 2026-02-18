package agent

import (
	"testing"
	"time"
)

func TestIdleMonitor(t *testing.T) {
	t.Parallel()

	idleTimeout := 100 * time.Millisecond
	i := NewIdleMonitor(t.Context(), 3, idleTimeout)

	// These "agents" don't actually run, they're just 3 different pointers.
	agents := []*AgentWorker{
		new(AgentWorker), new(AgentWorker), new(AgentWorker),
	}

	i.MarkBusy(agents[0])
	i.MarkIdle(agents[1])
	i.MarkDead(agents[2])

	// The idle monitor should start exiting within 1 second of the agents all
	// being idle or dead.
	start := time.Now()
	i.MarkIdle(agents[0])
	select {
	case <-i.Exiting():
		// This case should win, but only after the timeout.
		if exitedAfter := time.Since(start); exitedAfter < idleTimeout {
			t.Errorf("exitedAfter = %v, want > %v", exitedAfter, idleTimeout)
		}

	case <-time.After(2 * idleTimeout):
		// TODO: use testing/synctest when that becomes available
		t.Error("timed out waiting on <-i.Exiting()")
	}
}

func TestIdleMonitor_AllDead(t *testing.T) {
	t.Parallel()

	idleTimeout := 100 * time.Millisecond
	i := NewIdleMonitor(t.Context(), 3, idleTimeout)

	agents := []*AgentWorker{
		new(AgentWorker), new(AgentWorker), new(AgentWorker),
	}

	// All agents dead should result in exiting instantly.
	i.MarkDead(agents[0])
	i.MarkDead(agents[1])

	start := time.Now()
	i.MarkDead(agents[2])

	select {
	case <-i.Exiting():
		// This case should win, quickly.
		if exitedAfter := time.Since(start); exitedAfter > idleTimeout {
			t.Errorf("exitedAfter = %v, want < %v", exitedAfter, idleTimeout)
		}
	case <-time.After(idleTimeout):
		// TODO: use testing/synctest when that becomes available
		t.Error("timed out waiting on <-i.Exiting()")
	}
}

func TestIdleMonitor_Busy(t *testing.T) {
	t.Parallel()

	idleTimeout := 100 * time.Millisecond
	i := NewIdleMonitor(t.Context(), 3, idleTimeout)

	agents := []*AgentWorker{
		new(AgentWorker), new(AgentWorker), new(AgentWorker),
	}

	// Any agent still busy should not cause an exit.
	i.MarkDead(agents[0])
	i.MarkDead(agents[1])
	i.MarkBusy(agents[2])

	select {
	case <-i.Exiting():
		t.Error("<-i.Exiting() happened while at least one agent was still busy")

	case <-time.After(2 * idleTimeout):
		// This case should win.
	}
}
