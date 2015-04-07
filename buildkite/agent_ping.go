package buildkite

import (
	"fmt"
)

type AgentPing struct {
	// The action that Buildkite wants this agent to do next time it checks
	// in
	Action string `json:"action"`

	// Any message that should be outputted in the logs
	Message string `json:"message"`

	// Any job that has been assigned to the agent
	Job *Job `json:"job"`
}

func (a *AgentPing) String() string {
	return fmt.Sprintf("AgentPing{Action: %s, Message: %s, Job: %s}", a.Action, a.Message, a.Job)
}

func (c *Client) AgentPing() (*AgentPing, error) {
	// Create a new instance of a ping that will be populated by the client.
	var ping AgentPing

	// Return the ping.
	return &ping, c.Get(&ping, "ping")
}
