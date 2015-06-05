package buildkite

type Ping struct {
	// The Agent making the ping
	Agent *Agent

	// The action that Buildkite wants this agent to do next time it checks
	// in
	Action string `json:"action"`

	// Any message that should be outputted in the logs
	Message string `json:"message"`

	// Any job that has been assigned to the agent
	Job *Job `json:"job"`
}

func (p *Ping) Perform() error {
	return p.Agent.API.Get("/ping", &p)
}
