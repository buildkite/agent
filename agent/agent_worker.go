package agent

import (
	"expvar"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/buildkite/agent/api"
	"github.com/buildkite/agent/logger"
	"github.com/buildkite/agent/proctitle"
	"github.com/buildkite/agent/retry"
)

type AgentWorker struct {
	// The API Client used when this agent is communicating with the API
	APIClient *api.Client

	// The endpoint that should be used when communicating with the API
	Endpoint string

	// The registred agent API record
	Agent *api.Agent

	// The configuration of the agent from the CLI
	AgentConfiguration *AgentConfiguration

	// Whether or not the agent is running
	running bool

	// Used by the Start call to control the looping of the pings
	ticker *time.Ticker

	// Tracking the auto disconnect timer
	disconnectTimeoutTimer *time.Timer

	// Stop controls
	stop      chan struct{}
	stopping  bool
	stopMutex sync.Mutex

	// When this worker runs a job, we'll store an instance of the
	// JobRunner here
	jobRunner *JobRunner

	// Tracks the last successful heartbeat and ping
	lastPing, lastHeartbeat int64

	// Metrics that the worker exposes
	heartbeatMetrics, pingMetrics *expvar.Map
}

// Creates the agent worker and initializes it's API Client
func (a AgentWorker) Create() AgentWorker {
	var endpoint string
	if a.Agent.Endpoint != "" {
		endpoint = a.Agent.Endpoint
	} else {
		endpoint = a.Endpoint
	}

	a.APIClient = APIClient{Endpoint: endpoint, Token: a.Agent.AccessToken}.Create()

	// create counters for metrics
	a.heartbeatMetrics = expvar.NewMap("heartbeats")
	a.pingMetrics = expvar.NewMap("pings")

	return a
}

// Starts the agent worker
func (a *AgentWorker) Start() error {
	// Mark the agent as running
	a.running = true

	// Create the intervals we'll be using
	pingInterval := time.Second * time.Duration(a.Agent.PingInterval)
	heartbeatInterval := time.Second * time.Duration(a.Agent.HearbeatInterval)

	// Setup and start the heartbeater
	go func() {
		// Keep the heartbeat running as long as the agent is
		for a.running {
			err := a.Heartbeat()
			if err != nil {
				// Get the last heartbeat time to the nearest microsecond
				lastHeartbeat := time.Unix(atomic.LoadInt64(&a.lastPing), 0)

				// Track metrics
				a.heartbeatMetrics.Add("Fail", 1)

				logger.Error("Failed to heartbeat %s. Will try again in %s. (Last successful was %v ago)",
					err, heartbeatInterval, time.Now().Sub(lastHeartbeat))
			}

			time.Sleep(heartbeatInterval)
		}
	}()

	// Create the ticker and stop channels
	a.ticker = time.NewTicker(pingInterval)
	a.stop = make(chan struct{})

	// Setup a timer to automatically disconnect if no job has started
	if a.AgentConfiguration.DisconnectAfterJob {
		a.disconnectTimeoutTimer = time.NewTimer(time.Second * time.Duration(a.AgentConfiguration.DisconnectAfterJobTimeout))
		go func() {
			<-a.disconnectTimeoutTimer.C
			logger.Debug("[DisconnectionTimer] Reached %d seconds...", a.AgentConfiguration.DisconnectAfterJobTimeout)

			// Just double check that the agent isn't running a
			// job. The timer is stopped just after this is
			// assigned, but there's a potential race condition
			// where in between accepting the job, and creating the
			// `jobRunner`, the timer pops.
			if a.jobRunner == nil && !a.stopping {
				logger.Debug("[DisconnectionTimer] The agent isn't running a job, going to signal a stop")
				a.Stop(true)
			} else {
				logger.Debug("[DisconnectionTimer] Agent is running a job, going to just ignore and let it finish it's work")
			}
		}()

		logger.Debug("[DisconnectionTimer] Started for %d seconds...", a.AgentConfiguration.DisconnectAfterJobTimeout)
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
			return nil
		}
	}

	// Mark the agent as not running anymore
	a.running = false

	return nil
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
			logger.Warn("Agent is already gracefully stopping...")
		} else {
			// If we have a job, tell the user that we'll wait for
			// it to finish before disconnecting
			if a.jobRunner != nil {
				logger.Info("Gracefully stopping agent. Waiting for current job to finish before disconnecting...")
			} else {
				logger.Info("Gracefully stopping agent. Since there is no job running, the agent will disconnect immediately")
			}
		}
	} else {
		// If there's a job running, kill it, then disconnect
		if a.jobRunner != nil {
			logger.Info("Forcefully stopping agent. The current job will be canceled before disconnecting...")

			// Kill the current job. Doesn't do anything if the job
			// is already being killed, so it's safe to call
			// multiple times.
			a.jobRunner.Kill()
		} else {
			logger.Info("Forcefully stopping agent. Since there is no job running, the agent will disconnect immediately")
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
	if a.ticker != nil {
		close(a.stop)
	}

	// Mark the agent as stopping
	a.stopping = true
}

// Connects the agent to the Buildkite Agent API, retrying up to 30 times if it
// fails.
func (a *AgentWorker) Connect() error {
	// Update the proc title
	a.UpdateProcTitle("connecting")

	return retry.Do(func(s *retry.Stats) error {
		_, err := a.APIClient.Agents.Connect()
		if err != nil {
			logger.Warn("%s (%s)", err, s)
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
		beat, _, err = a.APIClient.Heartbeats.Beat()
		if err != nil {
			logger.Warn("%s (%s)", err, s)
		}
		return err
	}, &retry.Config{Maximum: 5, Interval: 5 * time.Second})

	if err != nil {
		return err
	}

	// Track a timestamp for the successful heartbeat for better errors
	atomic.StoreInt64(&a.lastHeartbeat, time.Now().Unix())

	// Track metrics
	a.heartbeatMetrics.Add("Total", 1)
	a.heartbeatMetrics.Add("Success", 1)

	logger.Debug("Heartbeat sent at %s and received at %s", beat.SentAt, beat.ReceivedAt)
	return nil
}

// Performs a ping, which returns what action the agent should take next.
func (a *AgentWorker) Ping() {
	// Update the proc title
	a.UpdateProcTitle("pinging")

	ping, _, err := a.APIClient.Pings.Get()
	if err != nil {
		// Get the last ping time to the nearest microsecond
		lastPing := time.Unix(atomic.LoadInt64(&a.lastPing), 0)

		// If a ping fails, we don't really care, because it'll
		// ping again after the interval.
		logger.Warn("Failed to ping: %s (Last successful was %v ago)", err, time.Now().Sub(lastPing))

		// When the ping fails, we wan't to reset our disconnection
		// timer. It wouldnt' be very nice if we just killed the agent
		// because Buildkite was having some connection issues.
		if a.disconnectTimeoutTimer != nil {
			jobTimeoutSeconds := time.Second * time.Duration(a.AgentConfiguration.DisconnectAfterJobTimeout)
			a.disconnectTimeoutTimer.Reset(jobTimeoutSeconds)

			logger.Debug("[DisconnectionTimer] Reset back to %d seconds because of ping failure...", a.AgentConfiguration.DisconnectAfterJobTimeout)
		}

		// Track metrics
		a.pingMetrics.Add("Fail", 1)

		return
	} else {
		// Track a timestamp for the successful ping for better errors
		atomic.StoreInt64(&a.lastPing, time.Now().Unix())

		// Track metrics
		a.pingMetrics.Add("Total", 1)
		a.pingMetrics.Add("Success", 1)
	}

	// Should we switch endpoints?
	if ping.Endpoint != "" && ping.Endpoint != a.Agent.Endpoint {
		// Before switching to the new one, do a ping test to make sure it's
		// valid. If it is, switch and carry on, otherwise ignore the switch
		// for now.
		newAPIClient := APIClient{Endpoint: ping.Endpoint, Token: a.Agent.AccessToken}.Create()
		newPing, _, err := newAPIClient.Pings.Get()
		if err != nil {
			logger.Warn("Failed to ping the new endpoint %s - ignoring switch for now (%s)", ping.Endpoint, err)
		} else {
			// Replace the APIClient and process the new ping
			a.APIClient = newAPIClient
			a.Agent.Endpoint = ping.Endpoint
			ping = newPing
		}
	}

	// Is there a message that should be shown in the logs?
	if ping.Message != "" {
		logger.Info(ping.Message)
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

	logger.Info("Assigned job %s. Accepting...", ping.Job.ID)

	// Accept the job. We'll retry on connection related issues, but if
	// Buildkite returns a 422 or 500 for example, we'll just bail out,
	// re-ping, and try the whole process again.
	var accepted *api.Job
	retry.Do(func(s *retry.Stats) error {
		accepted, _, err = a.APIClient.Jobs.Accept(ping.Job)

		if err != nil {
			if api.IsRetryableError(err) {
				logger.Warn("%s (%s)", err, s)
			} else {
				logger.Warn("Buildkite rejected the call to accept the job (%s)", err)
				s.Break()
			}
		}

		return err
	}, &retry.Config{Maximum: 30, Interval: 5 * time.Second})

	// If `accepted` is nil, then the job was never accepted
	if accepted == nil {
		logger.Error("Failed to accept job")
		return
	}

	// Now that the job has been accepted, we can start it.
	a.jobRunner, err = JobRunner{
		Endpoint:           accepted.Endpoint,
		Agent:              a.Agent,
		AgentConfiguration: a.AgentConfiguration,
		Job:                accepted,
	}.Create()

	// Woo! We've got a job, and successfully accepted it, let's kill our auto-disconnect timer
	if a.disconnectTimeoutTimer != nil {
		logger.Debug("[DisconnectionTimer] A job was assigned and accepted, stopping timer...")
		a.disconnectTimeoutTimer.Stop()
	}

	// Was there an error creating the job runner?
	if err != nil {
		logger.Error("Failed to initialize job: %s", err)
		return
	}

	// Start running the job
	if err = a.jobRunner.Run(); err != nil {
		logger.Error("Failed to run job: %s", err)
	}

	// No more job, no more runner.
	a.jobRunner = nil

	if a.AgentConfiguration.DisconnectAfterJob {
		logger.Info("Job finished. Disconnecting...")

		// We can just kill this timer now as well
		if a.disconnectTimeoutTimer != nil {
			a.disconnectTimeoutTimer.Stop()
		}

		// Tell the agent to finish up
		a.Stop(true)
	}
}

// Disconnects the agent from the Buildkite Agent API, doesn't bother retrying
// because we want to disconnect as fast as possible.
func (a *AgentWorker) Disconnect() error {
	// Update the proc title
	a.UpdateProcTitle("disconnecting")

	_, err := a.APIClient.Agents.Disconnect()
	if err != nil {
		logger.Warn("There was an error sending the disconnect API call to Buildkite. If this agent still appears online, you may have to manually stop it (%s)", err)
	}

	return err
}

func (a *AgentWorker) UpdateProcTitle(action string) {
	proctitle.Replace(fmt.Sprintf("buildkite-agent v%s [%s]", Version(), action))
}
