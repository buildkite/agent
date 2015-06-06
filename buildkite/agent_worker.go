package buildkite

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

	// Used by the Start call to control the looping of the pings
	ticker *time.Ticker
	stop   chan bool
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

	for {
		// Perform the ping
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

// Stops the agent from accepting new work
func (a *AgentWorker) Stop() {
	// If we have a ticker, stop it, and send a signal to the stop channel,
	// which will cause the agent worker to stop looping immediatly.
	if a.ticker != nil {
		a.stop <- true
		a.ticker.Stop()
	}
}

// Connects the agent to the Buildkite Agent API
func (a *AgentWorker) Connect() error {
	connector := func(s *retry.Stats) error {
		_, err := a.APIClient.Agents.Connect()
		if err != nil {
			logger.Warn("%s (%s)", err, s)
		}

		return err
	}

	return retry.Do(connector, &retry.Config{Maximum: 30})
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

	// Do nothing if there's no job
	if ping.Job == nil {
		return
	}

	// Accept the job
	logger.Info("Assigned job %s. Accepting...", ping.Job.ID)
	accepted, _, err := a.APIClient.Jobs.Accept(ping.Job)
	if err != nil {
		logger.Error("Failed to accept the job (%s)", err)
		return
	}

	logger.Debug("%+v", ping)

	//job := ping.Job
	//job.API = agent.API

	//jobRunner := JobRunner{
	//	Job:                            job,
	//	Agent:                          agent,
	//	BootstrapScript:                r.BootstrapScript,
	//	BuildPath:                      r.BuildPath,
	//	HooksPath:                      r.HooksPath,
	//	AutoSSHFingerprintVerification: r.AutoSSHFingerprintVerification,
	//	CommandEval:                    r.CommandEval,
	//	RunInPty:                       r.RunInPty,
	//}

	//r.jobRunner = &jobRunner

	//err = r.jobRunner.Run()
	//if err != nil {
	//	logger.Error("Failed to run job: %s", err)
	//}

	//r.jobRunner = nil
}

func (a *AgentWorker) Disconnect() error {
	disconnector := func(s *retry.Stats) error {
		_, err := a.APIClient.Agents.Disconnect()
		if err != nil {
			logger.Warn("%s (%s)", err, s)
		}

		return err
	}

	return retry.Do(disconnector, &retry.Config{Maximum: 30})
}
