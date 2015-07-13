package agent

import (
	"sync"
	"time"

	"github.com/buildkite/agent/api"
	"github.com/buildkite/agent/logger"
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

	// Stop controls
	stop      chan struct{}
	stopping  bool
	stopMutex sync.Mutex

	// When this worker runs a job, we'll store an instance of the
	// JobRunner here
	jobRunner *JobRunner
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

	return a
}

// Starts the agent worker
func (a *AgentWorker) Start() error {
	// Mark the agent as running
	a.running = true

	// Create the intervals we'll be using
	pingInterval := 5 * time.Second
	heartbeatInterval := 30 * time.Second

	// Setup the heartbeat runner
	go func() {
		// Keep the heartbeat running as long as the agent is
		for a.running {
			// Retry the heartbeat a few times
			err := retry.Do(func(s *retry.Stats) error {
				_, _, err := a.APIClient.Heartbeats.Beat()
				if err != nil {
					logger.Warn("%s (%s)", err, s)
				}
				return err
			}, &retry.Config{Maximum: 5, Interval: 1 * time.Second})
			if err != nil {
				logger.Error("Failed to heartbeat %s. Will try again in %s", err, heartbeatInterval)
			}

			// Wait for x to heartbeat again
			time.Sleep(heartbeatInterval)
		}
	}()

	// Create the ticker and stop channels
	a.ticker = time.NewTicker(pingInterval)
	a.stop = make(chan struct{})

	// Continue this loop until the the ticker is stopped, and we received
	// a message on the stop channel.
	for {
		a.Ping()

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
func (a *AgentWorker) Stop() {
	// Only allow one stop to run at a time (because we're playing with channels)
	a.stopMutex.Lock()

	if a.stopping {
		logger.Debug("Agent is already stopping...")
		return
	} else {
		logger.Debug("Stopping the agent...")
	}

	// If ther'es a running job, kill it.
	if a.jobRunner != nil {
		a.jobRunner.Kill()
	}

	// If we have a ticker, stop it, and send a signal to the stop channel,
	// which will cause the agent worker to stop looping immediatly.
	if a.ticker != nil {
		close(a.stop)
	}

	// Mark the agent as stopping
	a.stopping = true

	// Unlock the stop mutex
	a.stopMutex.Unlock()
}

// Connects the agent to the Buildkite Agent API, retrying up to 30 times if it
// fails.
func (a *AgentWorker) Connect() error {
	return retry.Do(func(s *retry.Stats) error {
		_, err := a.APIClient.Agents.Connect()
		if err != nil {
			logger.Warn("%s (%s)", err, s)
		}

		return err
	}, &retry.Config{Maximum: 10, Interval: 1 * time.Second})
}

// Performs a ping, which returns what action the agent should take next.
func (a *AgentWorker) Ping() {
	ping, _, err := a.APIClient.Pings.Get()
	if err != nil {
		// If a ping fails, we don't really care, because it'll
		// ping again after the interval.
		logger.Warn("Failed to ping: %s", err)
		return
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
		a.Stop()
		return
	}

	// If we don't have a job, there's nothing to do!
	if ping.Job == nil {
		return
	}

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
	}, &retry.Config{Maximum: 30, Interval: 1 * time.Second})

	// Now that the job has been accepted, we can start it.
	a.jobRunner, err = JobRunner{
		Endpoint:           accepted.Endpoint,
		Agent:              a.Agent,
		AgentConfiguration: a.AgentConfiguration,
		Job:                accepted,
	}.Create()

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
}

// Disconnects the agent from the Buildkite Agent API, doesn't bother retrying
// because we want to disconnect as fast as possible.
func (a *AgentWorker) Disconnect() error {
	_, err := a.APIClient.Agents.Disconnect()
	if err != nil {
		logger.Warn("There was an error sending the disconnect API call to Buildkite. If this agent still appears online, you may have to manually stop it (%s)", err)
	}

	return err
}
