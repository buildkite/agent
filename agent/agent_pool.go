package agent

import (
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/buildkite/agent/logger"
)

// AgentPool manages multiple parallel AgentWorkers
type AgentPool struct {
	logger  logger.Logger
	workers []*AgentWorker
}

// NewAgentPool returns a new AgentPool
func NewAgentPool(l logger.Logger, workers []*AgentWorker) *AgentPool {
	return &AgentPool{
		logger:  l,
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

	// Listen for process signals
	r.watchWorkers()

	r.logger.Info("Started %d Agent(s)", spawn)
	r.logger.Info("You can press Ctrl-C to stop the agents")

	return <-errs
}

func (r *AgentPool) runWorker(worker *AgentWorker, im *IdleMonitor) error {
	// Connect the worker to the API
	if err := worker.Connect(); err != nil {
		return err
	}

	// Starts the agent worker and wait for it to finish
	if err := worker.Start(im); err != nil {
		return err
	}

	// Now that the agent has stopped, we can disconnect it
	if err := worker.Disconnect(); err != nil {
		return err
	}

	return nil
}

func (r *AgentPool) watchWorkers() {
	// Start a signalwatcher so we can monitor signals and handle shutdowns
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt,
		syscall.SIGHUP,
		syscall.SIGTERM,
		syscall.SIGINT,
		syscall.SIGQUIT)
	defer signal.Stop(signals)

	go func() {
		var interruptCount int

		for sig := range signals {
			r.logger.Debug("Received signal `%v`", sig)

			switch sig {
			case syscall.SIGQUIT:
				r.logger.Debug("Received signal `%s`", sig.String())
				for _, worker := range r.workers {
					worker.Stop(false)
				}
			case syscall.SIGTERM, syscall.SIGINT:
				r.logger.Debug("Received signal `%s`", sig.String())
				if interruptCount == 0 {
					interruptCount++
					r.logger.Info("Received CTRL-C, send again to forcefully kill the agent(s)")
					for _, worker := range r.workers {
						worker.Stop(true)
					}
				} else {
					r.logger.Info("Forcefully stopping running jobs and stopping the agent(s)")
					for _, worker := range r.workers {
						worker.Stop(false)
					}
				}
			default:
				r.logger.Debug("Ignoring signal `%s`", sig.String())
			}
		}
	}()
}
