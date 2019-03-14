package agent

import (
	"sync"

	"github.com/buildkite/agent/api"
	"github.com/buildkite/agent/logger"
	"github.com/buildkite/agent/signalwatcher"
)

// AgentPool registers and spawns multiple parallel agent workers
// and manages their execution lifecycles
type AgentPool struct {
	reg            *Registrar
	logger         *logger.Logger
	workerFunc     func(*api.Agent) *AgentWorker
	interruptCount int
	signalLock     sync.Mutex
}

// NewAgentPool returns a new AgentPool
func NewAgentPool(l *logger.Logger, r *Registrar, f func(*api.Agent) *AgentWorker) *AgentPool {
	return &AgentPool{
		logger:     l,
		reg:        r,
		workerFunc: f,
	}
}

// Start kicks off the parallel registration and running of AgentWorkers
func (r *AgentPool) Start(spawn int) error {
	var wg sync.WaitGroup
	var errs = make(chan error, spawn)

	for i := 0; i < spawn; i++ {
		if spawn == 1 {
			r.logger.Info("Registering agent with Buildkite...")
		} else {
			r.logger.Info("Registering agent %d of %d with Buildkite...", i+1, spawn)
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := r.startWorker(); err != nil {
				errs <- err
			}
		}()
	}

	go func() {
		wg.Wait()
		close(errs)
	}()

	r.logger.Info("Started %d Agent(s)", spawn)
	r.logger.Info("You can press Ctrl-C to stop the agents")

	return <-errs
}

func (r *AgentPool) startWorker() error {
	registered, err := r.reg.Register()
	if err != nil {
		return err
	}

	// Now that we have a registered agent, we can connect it to the API,
	// and start running jobs.
	worker := r.workerFunc(registered)

	if err := worker.Connect(); err != nil {
		return err
	}

	// Start a signalwatcher so we can monitor signals and handle shutdowns
	signalwatcher.Watch(func(sig signalwatcher.Signal) {
		r.signalLock.Lock()
		defer r.signalLock.Unlock()

		if sig == signalwatcher.QUIT {
			r.logger.Debug("Received signal `%s`", sig.String())
			worker.Stop(false)
		} else if sig == signalwatcher.TERM || sig == signalwatcher.INT {
			r.logger.Debug("Received signal `%s`", sig.String())
			if r.interruptCount == 0 {
				r.interruptCount++
				r.logger.Info("Received CTRL-C, send again to forcefully kill the agent")
				worker.Stop(true)
			} else {
				r.logger.Info("Forcefully stopping running jobs and stopping the agent")
				worker.Stop(false)
			}
		} else {
			r.logger.Debug("Ignoring signal `%s`", sig.String())
		}
	})

	// Starts the agent worker.
	if err := worker.Start(); err != nil {
		return err
	}

	// Now that the agent has stopped, we can disconnect it
	if err := worker.Disconnect(); err != nil {
		return err
	}

	return nil
}
