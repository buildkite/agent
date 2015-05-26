package buildkite

import (
	"fmt"
)

type Ping struct {
	// The action that Buildkite wants this agent to do next time it checks
	// in
	Action string `json:"action"`

	// Any message that should be outputted in the logs
	Message string `json:"message"`

	// Any job that has been assigned to the agent
	Job *Job `json:"job"`
}

func (a *Ping) String() string {
	return fmt.Sprintf("Ping{Action: %s, Message: %s, Job: %s}", a.Action, a.Message, a.Job)
}
