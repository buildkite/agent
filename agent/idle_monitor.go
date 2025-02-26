package agent

import "sync"

// This monitor has a 3rd implicit state we will call "initializing" that all agents start in
// Agents can transition to busy and/or idle but always start in the "initializing" state
/*
//                -> Busy
//              /     ^
// Initializing       |
//              \     v
//                -> Idle
*/
// This (intentionally?) ensures the DisconnectAfterIdleTimeout doesn't fire before agents have had a chance to run a job
type IdleMonitor struct {
	sync.Mutex
	totalAgents int
	idle        map[string]struct{}
}

func NewIdleMonitor(totalAgents int) *IdleMonitor {
	return &IdleMonitor{
		totalAgents: totalAgents,
		idle:        map[string]struct{}{},
	}
}

func (i *IdleMonitor) Idle() bool {
	i.Lock()
	defer i.Unlock()
	return len(i.idle) == i.totalAgents
}

func (i *IdleMonitor) MarkIdle(agentUUID string) {
	i.Lock()
	defer i.Unlock()
	i.idle[agentUUID] = struct{}{}
}

func (i *IdleMonitor) MarkBusy(agentUUID string) {
	i.Lock()
	defer i.Unlock()
	delete(i.idle, agentUUID)
}
