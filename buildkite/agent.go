package buildkite

import (
	"fmt"
	"os"
	"time"
)

type Agent struct {
	// The name of the agent
	Name string

	// The client the agent will use to communicate to
	// the API
	Client Client

	// The PID of the agent
	PID int `json:"pid,omitempty"`

	// The hostname of the agent
	Hostname string `json:"hostname,omitempty"`

	// The boostrap script to run
	BootstrapScript string

	// The path to the run the builds in
	BuildPath string

	// Where bootstrap hooks are found
	HooksPath string

	// Whether or not the agent is allowed to automatically accept SSH
	// fingerprints
	AutoSSHFingerprintVerification bool

	// If this agent is allowed to perform script evaluation
	ScriptEval bool

	// Run jobs in a PTY
	RunInPty bool

	// The currently running Job
	Job *Job

	// This is true if the agent should no longer accept work
	stopping bool
}

func (c *Client) AgentConnect(agent *Agent) error {
	return c.Post(&agent, "/connect", agent)
}

func (c *Client) AgentDisconnect(agent *Agent) error {
	return c.Post(&agent, "/disconnect", agent)
}

func (a *Agent) String() string {
	return fmt.Sprintf("Agent{Name: %s, Hostname: %s, PID: %d, RunInPty: %t}", a.Name, a.Hostname, a.PID, a.RunInPty)
}

func (a *Agent) Setup() {
	// Set the PID of the agent
	a.PID = os.Getpid()

	// Get agent information from API. It will populate the
	// current agent struct with data.
	err := a.Client.AgentConnect(a)
	if err != nil {
		Logger.Fatal(err)
	}

	Logger.Infof("Agent successfully connected. Ready for work...")
}

func (a *Agent) Start() {
	// How long the agent will wait when no jobs can be found.
	idleSeconds := 5
	sleepTime := time.Duration(idleSeconds*1000) * time.Millisecond

	for {
		// Did the agent try and stop during the last job run?
		if a.stopping {
			a.Stop()
		} else {
			a.performNextJob()
		}

		// Sleep for a while before we check again
		time.Sleep(sleepTime)
	}
}

func (a *Agent) Stop() {
	// Disconnect from Buildkite
	Logger.Info("Disconnecting...")
	a.Client.AgentDisconnect(a)

	// Kill the process
	os.Exit(0)
}

func (a *Agent) performNextJob() {
	job, err := a.Client.JobNext()
	if err != nil {
		Logger.Warningf("Failed to get job (%s)", err)
		return
	}

	// If there's no ID, then there's no job.
	if job.ID == "" {
		return
	}

	Logger.Infof("Assigned job %s. Accepting...", job.ID)

	// Accept the job
	job, err = a.Client.JobAccept(job)
	if err != nil {
		Logger.Errorf("Failed to accept the job (%s)", err)
		return
	}

	// Confirm that it's been accepted
	if job.State != "accepted" {
		Logger.Errorf("Can not accept job with state `%s`", job.State)
		return
	}

	a.Job = job
	job.Run(a)
	a.Job = nil
}
