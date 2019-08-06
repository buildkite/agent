package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/buildkite/agent/api"
	"github.com/buildkite/agent/logger"
	"github.com/buildkite/agent/metrics"
	"github.com/buildkite/agent/process"
	"github.com/buildkite/agent/proctitle"
	"github.com/buildkite/agent/retry"
)

type AgentWorkerConfig struct {
	// Whether to set debug in the job
	Debug bool

	// What signal to use for worker cancellation
	CancelSignal process.Signal

	// The configuration of the agent from the CLI
	AgentConfiguration AgentConfiguration
}

type AgentWorker struct {
	// Tracks the last successful heartbeat and ping
	// NOTE: to avoid alignment issues on ARM architectures when
	// using atomic.StoreInt64 we need to keep this at the beginning
	// of the struct
	lastPing, lastHeartbeat int64

	// The API Client used when this agent is communicating with the API
	apiClient APIClient

	// The logger instance to use
	logger logger.Logger

	// The configuration of the agent from the CLI
	agentConfiguration AgentConfiguration

	// The registered agent API record
	agent *api.AgentRegisterResponse

	// Metric collection for the agent
	metricsCollector *metrics.Collector

	// Metrics scope for the agent
	metrics *metrics.Scope

	// Whether to enable debug
	debug bool

	// The signal to use for cancellation
	cancelSig process.Signal

	// Stop controls
	stop      chan struct{}
	stopping  bool
	stopMutex sync.Mutex

	// When this worker runs a job, we'll store an instance of the
	// JobRunner here
	jobRunner *JobRunner
}

// Creates the agent worker and initializes it's API Client
func NewAgentWorker(l logger.Logger, a *api.AgentRegisterResponse, m *metrics.Collector, apiClient APIClient, c AgentWorkerConfig) *AgentWorker {
	return &AgentWorker{
		logger:             l,
		agent:              a,
		metricsCollector:   m,
		apiClient:          apiClient.FromAgentRegisterResponse(a),
		debug:              c.Debug,
		agentConfiguration: c.AgentConfiguration,
		stop:               make(chan struct{}),
		cancelSig:          c.CancelSignal,
	}
}

// Starts the agent worker
func (a *AgentWorker) Start(idleMonitor *IdleMonitor) error {
	a.metrics = a.metricsCollector.Scope(metrics.Tags{
		"agent_name": a.agent.Name,
	})

	// Start running our metrics collector
	if err := a.metricsCollector.Start(); err != nil {
		return err
	}
	defer a.metricsCollector.Stop()

	// Create the intervals we'll be using
	pingInterval := time.Second * time.Duration(a.agent.PingInterval)
	heartbeatInterval := time.Second * time.Duration(a.agent.HeartbeatInterval)

	// Create the ticker
	pingTicker := time.NewTicker(pingInterval)
	defer pingTicker.Stop()

	// Use a context to run heartbeats for as long as the agent runs for
	heartbeatCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup and start the heartbeater
	go func() {
		for {
			select {
			case <-time.After(heartbeatInterval):
				err := a.Heartbeat()
				if err != nil {
					// Get the last heartbeat time to the nearest microsecond
					lastHeartbeat := time.Unix(atomic.LoadInt64(&a.lastHeartbeat), 0)

					a.logger.Error("Failed to heartbeat %s. Will try again in %s. (Last successful was %v ago)",
						err, heartbeatInterval, time.Now().Sub(lastHeartbeat))
				}

			case <-heartbeatCtx.Done():
				a.logger.Debug("Stopping heartbeats")
				return
			}
		}
	}()

	lastActionTime := time.Now()
	a.logger.Info("Waiting for work...")

	// Continue this loop until the closing of the stop channel signals termination
	for {
		if !a.stopping {
			job, err := a.Ping()
			if err != nil {
				a.logger.Warn("%v", err)
			} else if job != nil {
				// Let other agents know this agent is now busy and
				// not to idle terminate
				idleMonitor.MarkBusy(a.agent.UUID)

				// Runs the job, only errors if something goes wrong
				if runErr := a.AcceptAndRun(job); runErr != nil {
					a.logger.Error("%v", runErr)
				} else {
					if a.agentConfiguration.DisconnectAfterJob {
						a.logger.Info("Job finished. Disconnecting...")
						return nil
					}
					lastActionTime = time.Now()
				}
			}

			// Handle disconnect after idle timeout (and deprecated disconnect-after-job-timeout)
			if a.agentConfiguration.DisconnectAfterIdleTimeout > 0 {
				idleDeadline := lastActionTime.Add(time.Second *
					time.Duration(a.agentConfiguration.DisconnectAfterIdleTimeout))

				if time.Now().After(idleDeadline) {
					// Let other agents know this agent is now idle and termination
					// is possible
					idleMonitor.MarkIdle(a.agent.UUID)

					// But only terminate if everyone else is also idle
					if idleMonitor.Idle() {
						a.logger.Info("All agents have been idle for %d seconds. Disconnecting...",
							a.agentConfiguration.DisconnectAfterIdleTimeout)
						return nil
					} else {
						a.logger.Debug("Agent has been idle for %.f seconds, but other agents haven't",
							time.Now().Sub(lastActionTime).Seconds())
					}
				}
			}
		}

		select {
		case <-pingTicker.C:
			continue
		case <-a.stop:
			return nil
		}
	}
}

// Stops the agent from accepting new work and cancels any current work it's
// running
func (a *AgentWorker) Stop(graceful bool) {
	// Only allow one stop to run at a time (because we're playing with
	// channels)
	a.stopMutex.Lock()
	defer a.stopMutex.Unlock()

	if graceful {
		if a.stopping {
			a.logger.Warn("Agent is already gracefully stopping...")
		} else {
			// If we have a job, tell the user that we'll wait for
			// it to finish before disconnecting
			if a.jobRunner != nil {
				a.logger.Info("Gracefully stopping agent. Waiting for current job to finish before disconnecting...")
			} else {
				a.logger.Info("Gracefully stopping agent. Since there is no job running, the agent will disconnect immediately")
			}
		}
	} else {
		// If there's a job running, kill it, then disconnect
		if a.jobRunner != nil {
			a.logger.Info("Forcefully stopping agent. The current job will be canceled before disconnecting...")

			// Kill the current job. Doesn't do anything if the job
			// is already being killed, so it's safe to call
			// multiple times.
			a.jobRunner.Cancel()
		} else {
			a.logger.Info("Forcefully stopping agent. Since there is no job running, the agent will disconnect immediately")
		}
	}

	// We don't need to do the below operations again since we've already
	// done them before
	if a.stopping {
		return
	}

	// Update the proc title
	a.UpdateProcTitle("stopping")

	// Use the closure of the stop channel as a signal to the main run loop in Start()
	// to stop looping and terminate
	close(a.stop)

	// Mark the agent as stopping
	a.stopping = true
}

// Connects the agent to the Buildkite Agent API, retrying up to 30 times if it
// fails.
func (a *AgentWorker) Connect() error {
	a.logger.Info("Connecting to Buildkite...")

	// Update the proc title
	a.UpdateProcTitle("connecting")

	return retry.Do(func(s *retry.Stats) error {
		_, err := a.apiClient.Connect()
		if err != nil {
			a.logger.Warn("%s (%s)", err, s)
		}

		return err
	}, &retry.Config{Maximum: 10, Interval: 5 * time.Second})
}

// Performs a heatbeat
func (a *AgentWorker) Heartbeat() error {
	var beat *api.Heartbeat
	var err error

	// Retry the heartbeat a few times
	err = retry.Do(func(s *retry.Stats) error {
		beat, _, err = a.apiClient.Heartbeat()
		if err != nil {
			a.logger.Warn("%s (%s)", err, s)
		}
		return err
	}, &retry.Config{Maximum: 5, Interval: 5 * time.Second})

	if err != nil {
		return err
	}

	// Track a timestamp for the successful heartbeat for better errors
	atomic.StoreInt64(&a.lastHeartbeat, time.Now().Unix())

	a.logger.Debug("Heartbeat sent at %s and received at %s", beat.SentAt, beat.ReceivedAt)
	return nil
}

// Performs a ping that checks Buildkite for a job or action to take
// Returns a job, or nil if none is found
func (a *AgentWorker) Ping() (*api.Job, error) {
	// Update the proc title
	a.UpdateProcTitle("pinging")

	ping, _, err := a.apiClient.Ping()
	if err != nil {
		// Get the last ping time to the nearest microsecond
		lastPing := time.Unix(atomic.LoadInt64(&a.lastPing), 0)

		// If a ping fails, we don't really care, because it'll
		// ping again after the interval.
		return nil, fmt.Errorf("Failed to ping: %v (Last successful was %v ago)", err, time.Now().Sub(lastPing))
	}

	// Track a timestamp for the successful ping for better errors
	atomic.StoreInt64(&a.lastPing, time.Now().Unix())

	// Should we switch endpoints?
	if ping.Endpoint != "" && ping.Endpoint != a.agent.Endpoint {
		newAPIClient := a.apiClient.FromPing(ping)

		// Before switching to the new one, do a ping test to make sure it's
		// valid. If it is, switch and carry on, otherwise ignore the switch
		newPing, _, err := newAPIClient.Ping()
		if err != nil {
			a.logger.Warn("Failed to ping the new endpoint %s - ignoring switch for now (%s)", ping.Endpoint, err)
		} else {
			// Replace the APIClient and process the new ping
			a.apiClient = newAPIClient
			a.agent.Endpoint = ping.Endpoint
			ping = newPing
		}
	}

	// Is there a message that should be shown in the logs?
	if ping.Message != "" {
		a.logger.Info(ping.Message)
	}

	// Should the agent disconnect?
	if ping.Action == "disconnect" {
		a.Stop(false)
		return nil, nil
	}

	// If we don't have a job, there's nothing to do!
	if ping.Job == nil {
		// Update the proc title
		a.UpdateProcTitle("idle")
		return nil, nil
	}

	return ping.Job, nil
}

// Accepts a job and runs it, only returns an error if something goes wrong
func (a *AgentWorker) AcceptAndRun(job *api.Job) error {
	a.UpdateProcTitle(fmt.Sprintf("job %s", strings.Split(job.ID, "-")[0]))

	a.logger.Info("Assigned job %s. Accepting...", job.ID)

	// Accept the job. We'll retry on connection related issues, but if
	// Buildkite returns a 422 or 500 for example, we'll just bail out,
	// re-ping, and try the whole process again.
	var accepted *api.Job
	err := retry.Do(func(s *retry.Stats) error {
		var err error
		accepted, _, err = a.apiClient.AcceptJob(job)
		if err != nil {
			if api.IsRetryableError(err) {
				a.logger.Warn("%s (%s)", err, s)
			} else {
				a.logger.Warn("Buildkite rejected the call to accept the job (%s)", err)
				s.Break()
			}
		}

		return err
	}, &retry.Config{Maximum: 30, Interval: 5 * time.Second})

	// If `accepted` is nil, then the job was never accepted
	if accepted == nil {
		return fmt.Errorf("Failed to accept job: %v", err)
	}

	jobMetricsScope := a.metrics.With(metrics.Tags{
		`pipeline`: accepted.Env[`BUILDKITE_PIPELINE_SLUG`],
		`org`:      accepted.Env[`BUILDKITE_ORGANIZATION_SLUG`],
		`branch`:   accepted.Env[`BUILDKITE_BRANCH`],
		`source`:   accepted.Env[`BUILDKITE_SOURCE`],
	})

	defer func() {
		// No more job, no more runner.
		a.jobRunner = nil
	}()

	// Now that the job has been accepted, we can start it.
	a.jobRunner, err = NewJobRunner(a.logger, jobMetricsScope, a.agent, accepted, a.apiClient, JobRunnerConfig{
		Debug:              a.debug,
		CancelSignal:       a.cancelSig,
		AgentConfiguration: a.agentConfiguration,
	})

	// Was there an error creating the job runner?
	if err != nil {
		return fmt.Errorf("Failed to initialize job: %v", err)
	}

	// Start running the job
	if err = a.jobRunner.Run(); err != nil {
		return fmt.Errorf("Failed to run job: %v", err)
	}

	return nil
}

// Disconnects the agent from the Buildkite Agent API, doesn't bother retrying
// because we want to disconnect as fast as possible.
func (a *AgentWorker) Disconnect() error {
	a.logger.Info("Disconnecting...")

	// Update the proc title
	a.UpdateProcTitle("disconnecting")

	_, err := a.apiClient.Disconnect()
	if err != nil {
		a.logger.Warn("There was an error sending the disconnect API call to Buildkite. If this agent still appears online, you may have to manually stop it (%s)", err)
	}

	return err
}

func (a *AgentWorker) UpdateProcTitle(action string) {
	proctitle.Replace(fmt.Sprintf("buildkite-agent v%s [%s]", Version(), action))
}

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
