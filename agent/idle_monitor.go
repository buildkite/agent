package agent

import (
	"sync"
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
	mu          sync.Mutex
	exiting     bool
	totalAgents int
	idleAt      map[*AgentWorker]time.Time
}

// newIdleMonitor creates a new IdleMonitor.
func newIdleMonitor(totalAgents int) *idleMonitor {
	return &idleMonitor{
		totalAgents: totalAgents,
		idleAt:      make(map[*AgentWorker]time.Time),
	}
}

// shouldExit reports whether all agents are dead or have been idle for at least
// minIdle.  If shouldExit returns true, it will return true on all subsequent
// calls.
func (i *idleMonitor) shouldExit(minIdle time.Duration) bool {
	i.mu.Lock()
	defer i.mu.Unlock()

	// Once the idle monitor decides we're exiting, we're exiting.
	if i.exiting {
		return true
	}

	// Are all alive agents dead or idle for long enough?
	idle := 0
	for _, t := range i.idleAt {
		if !t.IsZero() && time.Since(t) < minIdle {
			return false
		}
		idle++
	}
	if idle < i.totalAgents {
		return false
	}
	i.exiting = true
	return true
}

// markIdle marks an agent as idle.
func (i *idleMonitor) markIdle(agent *AgentWorker) {
	i.mu.Lock()
	defer i.mu.Unlock()
	// Allow MarkIdle to be called multiple times without updating the idleAt
	// timestamp.
	if _, alreadyIdle := i.idleAt[agent]; alreadyIdle {
		return
	}
	i.idleAt[agent] = time.Now()
}

// markDead marks an agent as dead.
func (i *idleMonitor) markDead(agent *AgentWorker) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.idleAt[agent] = time.Time{}
}

// markBusy marks an agent as busy.
func (i *idleMonitor) markBusy(agent *AgentWorker) {
	i.mu.Lock()
	defer i.mu.Unlock()
	delete(i.idleAt, agent)
}
