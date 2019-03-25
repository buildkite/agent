package agent

import (
	"fmt"
	"os"
	"sync"

	"github.com/buildkite/agent/logger"
	"github.com/buildkite/agent/signalwatcher"
)

// AgentPool manages multiple parallel AgentWorkers
type AgentPool struct {
	logger  *logger.Logger
	workers []*AgentWorker
}

// NewAgentPool returns a new AgentPool
func NewAgentPool(l *logger.Logger, workers []*AgentWorker) *AgentPool {
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

	// Spawn goroutines for each parallel worker
	for _, worker := range r.workers {
		wg.Add(1)

		go func(worker *AgentWorker) {
			defer wg.Done()

			if err := r.runWorker(worker); err != nil {
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

func (r *AgentPool) runWorker(worker *AgentWorker) error {
	// Connect the worker to the API
	if err := worker.Connect(); err != nil {
		return err
	}

	// Starts the agent worker and wait for it to finish
	if err := worker.Start(); err != nil {
		return err
	}

	// Now that the agent has stopped, we can disconnect it
	if err := worker.Disconnect(); err != nil {
		return err
	}

	return nil
}

func (r *AgentPool) watchWorkers() {
	var signalLock sync.Mutex
	var interruptCount int

	signalwatcher.Watch(func(sig signalwatcher.Signal) {
		signalLock.Lock()
		defer signalLock.Unlock()

		if sig == signalwatcher.QUIT {
			r.logger.Debug("Received signal `%s`", sig.String())
			for _, worker := range r.workers {
				worker.Stop(false)
			}
		} else if sig == signalwatcher.TERM || sig == signalwatcher.INT {
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
		} else {
			r.logger.Debug("Ignoring signal `%s`", sig.String())
		}
	})
}

// ShowBanner prints a welcome banner and the configuration options
func ShowBanner(l *logger.Logger, conf AgentConfiguration) {
	welcomeMessage :=
		"\n" +
			"%s  _           _ _     _ _    _ _                                _\n" +
			" | |         (_) |   | | |  (_) |                              | |\n" +
			" | |__  _   _ _| | __| | | ___| |_ ___    __ _  __ _  ___ _ __ | |_\n" +
			" | '_ \\| | | | | |/ _` | |/ / | __/ _ \\  / _` |/ _` |/ _ \\ '_ \\| __|\n" +
			" | |_) | |_| | | | (_| |   <| | ||  __/ | (_| | (_| |  __/ | | | |_\n" +
			" |_.__/ \\__,_|_|_|\\__,_|_|\\_\\_|\\__\\___|  \\__,_|\\__, |\\___|_| |_|\\__|\n" +
			"                                                __/ |\n" +
			" http://buildkite.com/agent                    |___/\n%s\n"

	if !conf.DisableColors {
		fmt.Fprintf(os.Stderr, welcomeMessage, "\x1b[38;5;48m", "\x1b[0m")
	} else {
		fmt.Fprintf(os.Stderr, welcomeMessage, "", "")
	}

	l.Notice("Starting buildkite-agent v%s with PID: %s", Version(), fmt.Sprintf("%d", os.Getpid()))
	l.Notice("The agent source code can be found here: https://github.com/buildkite/agent")
	l.Notice("For questions and support, email us at: hello@buildkite.com")

	if conf.ConfigPath != "" {
		l.Info("Configuration loaded from: %s", conf.ConfigPath)
	}

	l.Debug("Bootstrap command: %s", conf.BootstrapScript)
	l.Debug("Build path: %s", conf.BuildPath)
	l.Debug("Hooks directory: %s", conf.HooksPath)
	l.Debug("Plugins directory: %s", conf.PluginsPath)

	if !conf.SSHKeyscan {
		l.Info("Automatic ssh-keyscan has been disabled")
	}

	if !conf.CommandEval {
		l.Info("Evaluating console commands has been disabled")
	}

	if !conf.PluginsEnabled {
		l.Info("Plugins have been disabled")
	}

	if !conf.RunInPty {
		l.Info("Running builds within a pseudoterminal (PTY) has been disabled")
	}

	if conf.DisconnectAfterJob {
		l.Info("Agent will disconnect after a job run has completed with a timeout of %d seconds",
			conf.DisconnectAfterJobTimeout)
	}
}
