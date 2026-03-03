package agent

import (
	"context"
	"time"
)

// idleMonitor tracks agent idleness, needed for "disconnect-after-idle" type
// logic.
//
// In addition to "busy", "idle", and "dead", idleMonitor has an implicit
// "initial" state. Agents always start in the "initial" state, but typically
// quickly transistion into either the idle or busy states (as soon as they
// have completed their first ping.)
/*
//                -> Busy --
//              /     ^      \
//      Initial ------+--------> Dead
//              \     v      /
//                -> Idle --
*/
type idleMonitor struct {
	// exiting is closed when the idle monitor says all agents should exit
	exiting chan struct{}

	// totalAgents is the total number of agents configured to run
	totalAgents int

	// idleTimeout is a copy of the DisconnectAfterIdleTimeout value
	idleTimeout time.Duration

	// Channels used to update the monitor state
	becameIdle chan *AgentWorker
	becameBusy chan *AgentWorker
	becameDead chan *AgentWorker

	// idleAt tracks when each agent became idle/dead.
	// Agents not present in the map are busy.
	idleAt map[*AgentWorker]time.Time
}

// NewIdleMonitor creates a new IdleMonitor.
func NewIdleMonitor(ctx context.Context, totalAgents int, idleTimeout time.Duration) *idleMonitor {
	if idleTimeout <= 0 {
		// Note that the methods handle a nil receiver safely.
		return nil
	}
	i := &idleMonitor{
		exiting:     make(chan struct{}),
		totalAgents: totalAgents,
		idleTimeout: idleTimeout,
		becameIdle:  make(chan *AgentWorker),
		becameBusy:  make(chan *AgentWorker),
		becameDead:  make(chan *AgentWorker),
		idleAt:      make(map[*AgentWorker]time.Time),
	}
	go i.monitor(ctx)
	return i
}

// monitor is the internal goroutine for handling idleness.
func (i *idleMonitor) monitor(ctx context.Context) {
	if i == nil {
		return
	}

	// Once the idle monitor returns, all the agents should also exit.
	defer close(i.exiting)

	var lastTimeout <-chan time.Time
	for {
		select {
		case <-ctx.Done():
			return

		case <-lastTimeout:
			return

		case agent := <-i.becameIdle:
			// Idleness is counted from when the agent first became idle.
			if _, alreadyIdle := i.idleAt[agent]; alreadyIdle {
				break
			}
			i.idleAt[agent] = time.Now()

		case agent := <-i.becameBusy:
			delete(i.idleAt, agent)

		case agent := <-i.becameDead:
			i.idleAt[agent] = time.Time{}
		}

		// Update the timeout channel based on all the agent states
		// Are there any busy agents? Then don't time out.
		if len(i.idleAt) < i.totalAgents {
			lastTimeout = nil
			continue
		}

		// They're all idle or dead. Figure out when the timeout should happen.
		// If they're all dead, then the timeout happens immediately.
		// If at least one is idle and _not_ dead, then the timeout happens
		// however much of idleTimeout remains since the agent that most
		// recently became idle.
		var timeout time.Duration
		for _, t := range i.idleAt {
			if t.IsZero() {
				continue
			}
			timeout = max(timeout, i.idleTimeout-time.Since(t))
		}
		if timeout == 0 {
			return
		}
		lastTimeout = time.After(timeout)
	}
}

// Exiting returns a channel that is closed when the monitor declares
// all agents should exit. It is safe to use with a nil pointer.
func (i *idleMonitor) Exiting() <-chan struct{} {
	if i == nil {
		return nil
	}
	return i.exiting
}

// MarkIdle marks an agent as idle. It is safe to use with a nil pointer.
func (i *idleMonitor) MarkIdle(agent *AgentWorker) {
	if i == nil {
		return
	}
	select {
	case i.becameIdle <- agent:
		// marked as idle
	case <-i.exiting:
		// no goroutine listening on i.becameIdle
	}
}

// MarkDead marks an agent as dead. It is safe to use with a nil pointer.
func (i *idleMonitor) MarkDead(agent *AgentWorker) {
	if i == nil {
		return
	}
	select {
	case i.becameDead <- agent:
		// marked as dead
	case <-i.exiting:
		// no goroutine listening on i.becameDead
	}
}

// MarkBusy marks an agent as busy. It is safe to use with a nil pointer.
func (i *idleMonitor) MarkBusy(agent *AgentWorker) {
	if i == nil {
		return
	}
	select {
	case i.becameBusy <- agent:
		// marked as busy
	case <-i.exiting:
		// no goroutine listening on i.becameBusy
	}
}
