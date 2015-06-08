package agent

import (
	"github.com/buildkite/agent/api"
	"github.com/buildkite/agent/logger"
	"github.com/buildkite/agent/retry"
	"time"
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

	// Used by the Start call to control the looping of the pings
	ticker *time.Ticker
	stop   chan bool

	// When this worker runs a job, we'll store an instance of the
	// JobRunner here
	jobRunner *JobRunner
}

// Creates the agent worker and initializes it's API Client
func (a AgentWorker) Create() AgentWorker {
	a.APIClient = APIClient{Endpoint: a.Endpoint, Token: a.Agent.AccessToken}.Create()

	return a
}

// Starts the agent worker
func (a *AgentWorker) Start() error {
	// Create the ticker and stop channels
	a.ticker = time.NewTicker(5 * time.Second)
	a.stop = make(chan bool, 1)

	// Continue this loop until the the ticker is stopped, and we received
	// a message on the stop channel.
	for {
		a.Ping()

		select {
		case <-a.ticker.C:
			continue
		case <-a.stop:
			return nil
		}
	}

	return nil
}

// Stops the agent from accepting new work and cancels any current work it's
// running
func (a *AgentWorker) Stop() {
	// If ther'es a running job, kill it.
	if a.jobRunner != nil {
		a.jobRunner.Kill()
	}

	// If we have a ticker, stop it, and send a signal to the stop channel,
	// which will cause the agent worker to stop looping immediatly.
	if a.ticker != nil {
		a.stop <- true
		a.ticker.Stop()
	}
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
	}, &retry.Config{Maximum: 10})
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

	// Accept the job. We don't bother retrying the accept. It it fails,
	// the ping will fail, and we'll just try this whole process all over
	// again.
	logger.Info("Assigned job %s. Accepting...", ping.Job.ID)
	accepted, _, err := a.APIClient.Jobs.Accept(ping.Job)
	if err != nil {
		logger.Error("Failed to accept the job (%s)", err)
		return
	}

	// Now that the job has been accepted, we can start it.
	a.jobRunner, err = JobRunner{
		Endpoint:           a.Endpoint,
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
