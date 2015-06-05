package buildkite

type Agent struct {
	// The agents API configuration
	API API

	// The name of the new agent
	Name string `json:"name"`

	// The access token for the agent
	AccessToken string `json:"access_token"`

	// Hostname of the machine
	Hostname string `json:"hostname"`

	// Operating system for this machine
	OS string `json:"os"`

	// If this agent is allowed to perform command evaluation
	CommandEval bool `json:"script_eval_enabled"`

	// The priority of the agent
	Priority string `json:"priority,omitempty"`

	// The version of this agent
	Version string `json:"version"`

	// Meta data for the agent
	MetaData []string `json:"meta_data"`

	// The PID of the agent
	PID int `json:"pid,omitempty"`

	// The boostrap script to run
	BootstrapScript string

	// The path to the run the builds in
	BuildPath string

	// Where bootstrap hooks are found
	HooksPath string

	// Whether or not the agent is allowed to automatically accept SSH
	// fingerprints
	AutoSSHFingerprintVerification bool

	// Run jobs in a PTY
	RunInPty bool

	// The currently running Job
	Job *Job

	// This is true if the agent should no longer accept work
	Stopping bool
}

func (a *Agent) Register() error {
	return a.API.Post("/register", &a, a)
}

func (a *Agent) Connect() error {
	return a.API.Post("/connect", &a, a)
}

func (a *Agent) Disconnect() error {
	return a.API.Post("/disconnect", &a, a)
}
