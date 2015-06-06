package buildkite

import (
	"github.com/buildkite/agent/api"
	"github.com/buildkite/agent/logger"
	"github.com/buildkite/agent/retry"
	_ "time"
)

type Agent struct {
	// The API Client used when this agent is communicating with the API
	APIClient *api.Client

	// The endpoint that should be used when communicating with the API
	Endpoint string

	// The registred agent API record
	Agent *api.Agent
}

func (a Agent) Create() Agent {
	a.APIClient = APIClient{Endpoint: a.Endpoint, Token: a.Agent.AccessToken}.Create()

	return a
}

func (a *Agent) Run() error {
	///////////////

	// How long the agent will wait when no jobs can be found.
	// idleSeconds := 5
	// sleepTime := time.Duration(idleSeconds*1000) * time.Millisecond

	// for {
	// 	// Did the agent try and stop during the last job run?
	// 	if r.stopping {
	// 		r.Stop(&agent)
	// 	} else {
	// 		r.Ping(&agent)
	// 	}

	// 	// Sleep for a while before we check again
	// 	time.Sleep(sleepTime)
	// }

	return nil
}

func (a *Agent) Connect() error {
	connector := func(s *retry.Stats) error {
		_, err := a.APIClient.Agents.Connect()
		if err != nil {
			logger.Warn("%s (%s)", err, s)
		}

		return err
	}

	return retry.Do(connector, &retry.Config{Maximum: 30})
}

func (a *Agent) Ping() {
	// Perform the ping
	//ping := Ping{Agent: agent}
	//err := ping.Perform()
	//if err != nil {
	//	logger.Warn("Failed to ping (%s)", err)
	//	return
	//}

	//// Is there a message that should be shown in the logs?
	//if ping.Message != "" {
	//	logger.Info(ping.Message)
	//}

	//// Should the agent disconnect?
	//if ping.Action == "disconnect" {
	//	r.Stop(agent)
	//	return
	//}

	//// Do nothing if there's no job
	//if ping.Job == nil {
	//	return
	//}

	//logger.Info("Assigned job %s. Accepting...", ping.Job.ID)

	//job := ping.Job
	//job.API = agent.API

	//// Accept the job
	//err = job.Accept()
	//if err != nil {
	//	logger.Error("Failed to accept the job (%s)", err)
	//	return
	//}

	//// Confirm that it's been accepted
	//if job.State != "accepted" {
	//	logger.Error("Can not accept job with state `%s`", job.State)
	//	return
	//}

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

func (a *Agent) Disconnect() {
	logger.Info("Disconnecting...")
}
