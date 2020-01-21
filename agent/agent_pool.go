package agent

import (
	"sync"
)

// AgentPool manages multiple parallel AgentWorkers
type AgentPool struct {
	workers []*AgentWorker
}

// NewAgentPool returns a new AgentPool
func NewAgentPool(workers []*AgentWorker) *AgentPool {
	return &AgentPool{
		workers: workers,
	}
}

// Start kicks off the parallel AgentWorkers and waits for them to finish
func (r *AgentPool) Start() error {
	var wg sync.WaitGroup
	var spawn int = len(r.workers)
	var errs = make(chan error, spawn)

	// Co-ordinate idle state across agents
	idleMonitor := NewIdleMonitor(len(r.workers))

	// Spawn goroutines for each parallel worker
	for _, worker := range r.workers {
		wg.Add(1)

		go func(worker *AgentWorker) {
			defer wg.Done()

			if err := r.runWorker(worker, idleMonitor); err != nil {
				errs <- err
			}
		}(worker)
	}

	go func() {
		wg.Wait()
		close(errs)
	}()

	return <-errs
}

func (r *AgentPool) runWorker(worker *AgentWorker, im *IdleMonitor) error {
	// Connect the worker to the API
	if err := worker.Connect(); err != nil {
		return err
	}
	// Ensure the worker is disconnected at the end of this function.
	defer worker.Disconnect()

	// Starts the agent worker and wait for it to finish.
	return worker.Start(im)
}

func (r *AgentPool) Stop(graceful bool) {
	for _, worker := range r.workers {
		worker.Stop(graceful)
	}
}
