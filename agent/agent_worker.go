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
	"github.com/buildkite/agent/proctitle"
	"github.com/buildkite/agent/retry"
)

type AgentWorkerConfig struct {
	// Whether to set debug in the job
	Debug bool

	// The endpoint that should be used when communicating with the API
	Endpoint string

	// Whether to disable http for the API
	DisableHTTP2 bool

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
	apiClient *api.Client

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

	// Whether or not the agent is running
	running bool

	// Used by the Start call to control the looping of the pings
	ticker *time.Ticker

	// Tracking the auto disconnect timer
	disconnectTimeoutTimer *time.Timer

	// Track the idle disconnect timer and a cross-agent monitor
	idleTimer   *time.Timer
	idleMonitor *IdleMonitor

	// Stop controls
	stop      chan struct{}
	stopping  bool
	stopMutex sync.Mutex

	// When this worker runs a job, we'll store an instance of the
	// JobRunner here
	jobRunner *JobRunner
}

// Creates the agent worker and initializes it's API Client
func NewAgentWorker(l logger.Logger, a *api.AgentRegisterResponse, m *metrics.Collector, c AgentWorkerConfig) *AgentWorker {
	var endpoint string
	if a.Endpoint != "" {
		endpoint = a.Endpoint
	} else {
		endpoint = c.Endpoint
	}

	// Create an APIClient with the agent's access token
	apiClient := NewAPIClient(l, APIClientConfig{
		Endpoint:     endpoint,
		Token:        a.AccessToken,
		DisableHTTP2: c.DisableHTTP2,
	})

	return &AgentWorker{
		logger:             l,
		agent:              a,
		metricsCollector:   m,
		apiClient:          apiClient,
		debug:              c.Debug,
		agentConfiguration: c.AgentConfiguration,
		stop:               make(chan struct{}),
	}
}

// Starts the agent worker
func (a *AgentWorker) Start(idle *IdleMonitor) error {
	a.metrics = a.metricsCollector.Scope(metrics.Tags{
		"agent_name": a.agent.Name,
	})

	// Start running our metrics collector
	if err := a.metricsCollector.Start(); err != nil {
		return err
	}
	defer a.metricsCollector.Stop()

	// Mark the agent as running
	a.running = true

	// Create the intervals we'll be using
	pingInterval := time.Second * time.Duration(a.agent.PingInterval)
	heartbeatInterval := time.Second * time.Duration(a.agent.HeartbeatInterval)

	// Create the ticker
	a.ticker = time.NewTicker(pingInterval)

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
					lastHeartbeat := time.Unix(atomic.LoadInt64(&a.lastPing), 0)

					a.logger.Error("Failed to heartbeat %s. Will try again in %s. (Last successful was %v ago)",
						err, heartbeatInterval, time.Now().Sub(lastHeartbeat))
				}

			case <-heartbeatCtx.Done():
				a.logger.Debug("Stopping heartbeats")
				return
			}
		}
	}()

	// Setup a timer to automatically disconnect if no job has started
	if a.agentConfiguration.DisconnectAfterJob {
		a.disconnectTimeoutTimer = time.NewTimer(time.Second * time.Duration(a.agentConfiguration.DisconnectAfterJobTimeout))
		go func() {
			<-a.disconnectTimeoutTimer.C
			a.logger.Debug("[DisconnectionTimer] Reached %d seconds...", a.agentConfiguration.DisconnectAfterJobTimeout)
			a.stopIfIdle()
		}()

		a.logger.Debug("[DisconnectionTimer] Started for %d seconds...", a.agentConfiguration.DisconnectAfterJobTimeout)
	}

	a.idleMonitor = idle

	// Setup an idle timer to disconnect after periods of idleness
	if a.agentConfiguration.DisconnectAfterIdleTimeout > 0 {
		a.idleTimer = time.NewTimer(time.Second * time.Duration(a.agentConfiguration.DisconnectAfterIdleTimeout))
		go func() {
			for {
				select {
				case <-a.idleTimer.C:
					// Mark this agent as idle in the shared idle monitor
					a.idleMonitor.MarkIdle(a.agent.UUID)

					// Only terminate if all agents in the pool are idle, otherwise extend the timer
					if a.idleMonitor.Idle() {
						a.logger.Info("Agent has been idle for %d seconds",
							a.agentConfiguration.DisconnectAfterIdleTimeout)
						a.stopIfIdle()
					} else {
						// Extend the timer by the smaller of 10% of the idle timer or 60 seconds
						extendDuration := (time.Second * time.Duration(a.agentConfiguration.DisconnectAfterIdleTimeout)) / 10
						if extendDuration > (time.Second * 60) {
							extendDuration = time.Second * 60
						}

						a.logger.Debug("Agent has been idle for %d seconds, but other agents are active so extending for %v",
							a.agentConfiguration.DisconnectAfterIdleTimeout, extendDuration)
						a.idleTimer.Reset(extendDuration)
					}

				case <-a.stop:
					a.logger.Debug("Stopping the idle ticker")
					return
				}
			}
		}()
	}

	if a.agentConfiguration.DisconnectAfterJob {
		a.logger.Info("Waiting for job to be assigned...")
		a.logger.Info("The agent will automatically disconnect after %d seconds if no job is assigned", a.agentConfiguration.DisconnectAfterJobTimeout)
	} else if a.agentConfiguration.DisconnectAfterIdleTimeout > 0 {
		a.logger.Info("Waiting for job to be assigned...")
		a.logger.Info("The agent will automatically disconnect after %d seconds of inactivity", a.agentConfiguration.DisconnectAfterIdleTimeout)
	} else {
		a.logger.Info("Waiting for work...")
	}

	// Continue this loop until the the ticker is stopped, and we received
	// a message on the stop channel.
	for {
		if !a.stopping {
			a.Ping()
		}

		select {
		case <-a.ticker.C:
			continue
		case <-a.stop:
			a.ticker.Stop()

			// Mark the agent as not running anymore
			a.running = false

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

	// If we have a ticker, stop it, and send a signal to the stop channel,
	// which will cause the agent worker to stop looping immediatly.
	close(a.stop)

	// Mark the agent as stopping
	a.stopping = true
}

func (a *AgentWorker) stopIfIdle() {
	if a.jobRunner == nil && !a.stopping {
		a.Stop(true)
	} else {
		a.logger.Debug("Agent is running a job, going to let it finish it's work")
	}
}

// Connects the agent to the Buildkite Agent API, retrying up to 30 times if it
// fails.
func (a *AgentWorker) Connect() error {
	a.logger.Info("Connecting to Buildkite...")

	// Update the proc title
	a.UpdateProcTitle("connecting")

	return retry.Do(func(s *retry.Stats) error {
		_, err := a.apiClient.Agents.Connect()
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
		beat, _, err = a.apiClient.Heartbeats.Beat()
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

// Performs a ping, which returns what action the agent should take next.
func (a *AgentWorker) Ping() {
	// Update the proc title
	a.UpdateProcTitle("pinging")

	ping, _, err := a.apiClient.Pings.Get()
	if err != nil {
		// Get the last ping time to the nearest microsecond
		lastPing := time.Unix(atomic.LoadInt64(&a.lastPing), 0)

		// If a ping fails, we don't really care, because it'll
		// ping again after the interval.
		a.logger.Warn("Failed to ping: %s (Last successful was %v ago)", err, time.Now().Sub(lastPing))

		// When the ping fails, we wan't to reset our disconnection
		// timer. It wouldnt' be very nice if we just killed the agent
		// because Buildkite was having some connection issues.
		if a.disconnectTimeoutTimer != nil {
			jobTimeoutSeconds := time.Second * time.Duration(a.agentConfiguration.DisconnectAfterJobTimeout)
			a.disconnectTimeoutTimer.Reset(jobTimeoutSeconds)

			a.logger.Debug("[DisconnectionTimer] Reset back to %d seconds because of ping failure...", a.agentConfiguration.DisconnectAfterJobTimeout)
		}

		return
	} else {
		// Track a timestamp for the successful ping for better errors
		atomic.StoreInt64(&a.lastPing, time.Now().Unix())
	}

	// Should we switch endpoints?
	if ping.Endpoint != "" && ping.Endpoint != a.agent.Endpoint {
		// Before switching to the new one, do a ping test to make sure it's
		// valid. If it is, switch and carry on, otherwise ignore the switch
		// for now.
		newAPIClient := NewAPIClient(a.logger, APIClientConfig{
			Endpoint: ping.Endpoint,
			Token:    a.agent.AccessToken,
		})

		newPing, _, err := newAPIClient.Pings.Get()
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
		return
	}

	// If we don't have a job, there's nothing to do!
	if ping.Job == nil {
		// Update the proc title
		a.UpdateProcTitle("idle")

		return
	}

	// Update the proc title
	a.UpdateProcTitle(fmt.Sprintf("job %s", strings.Split(ping.Job.ID, "-")[0]))

	a.logger.Info("Assigned job %s. Accepting...", ping.Job.ID)

	// Accept the job. We'll retry on connection related issues, but if
	// Buildkite returns a 422 or 500 for example, we'll just bail out,
	// re-ping, and try the whole process again.
	var accepted *api.Job
	retry.Do(func(s *retry.Stats) error {
		accepted, _, err = a.apiClient.Jobs.Accept(ping.Job)

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
		a.logger.Error("Failed to accept job")
		return
	}

	jobMetricsScope := a.metrics.With(metrics.Tags{
		`pipeline`: accepted.Env[`BUILDKITE_PIPELINE_SLUG`],
		`org`:      accepted.Env[`BUILDKITE_ORGANIZATION_SLUG`],
		`branch`:   accepted.Env[`BUILDKITE_BRANCH`],
		`source`:   accepted.Env[`BUILDKITE_SOURCE`],
	})

	// Now that the job has been accepted, we can start it.
	a.jobRunner, err = NewJobRunner(a.logger, jobMetricsScope, a.agent, accepted, JobRunnerConfig{
		Debug:              a.debug,
		Endpoint:           accepted.Endpoint,
		AgentConfiguration: a.agentConfiguration,
	})

	// Woo! We've got a job, and successfully accepted it, let's kill our auto-disconnect timer
	if a.disconnectTimeoutTimer != nil {
		a.logger.Debug("[DisconnectionTimer] A job was assigned and accepted, stopping timer...")
		a.disconnectTimeoutTimer.Stop()
	}

	// Was there an error creating the job runner?
	if err != nil {
		a.logger.Error("Failed to initialize job: %s", err)
		return
	}

	// Start running the job
	if err = a.jobRunner.Run(); err != nil {
		a.logger.Error("Failed to run job: %s", err)
	}

	// No more job, no more runner.
	a.jobRunner = nil

	if a.agentConfiguration.DisconnectAfterJob {
		a.logger.Info("Job finished. Disconnecting...")

		// We can just kill this timer now as well
		if a.disconnectTimeoutTimer != nil {
			a.disconnectTimeoutTimer.Stop()
		}

		// Tell the agent to finish up
		a.Stop(true)
	}

	if a.agentConfiguration.DisconnectAfterIdleTimeout > 0 {
		a.logger.Info("Job finished. Resetting idle timer...")
		a.idleTimer.Reset(time.Second * time.Duration(a.agentConfiguration.DisconnectAfterIdleTimeout))
		a.idleMonitor.MarkBusy(a.agent.Name)
	}
}

// Disconnects the agent from the Buildkite Agent API, doesn't bother retrying
// because we want to disconnect as fast as possible.
func (a *AgentWorker) Disconnect() error {
	a.logger.Info("Disconnecting...")

	// Update the proc title
	a.UpdateProcTitle("disconnecting")

	_, err := a.apiClient.Agents.Disconnect()
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
