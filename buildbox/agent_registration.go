package buildbox

import (
	"fmt"
)

type AgentRegistration struct {
	// The access token for the agent
	AccessToken string `json:"access_token"`

	// Hostname of the machine
	Hostname string `json:"hostname"`

	// The name of the new agent
	Name string `json:"name"`
}

func (c *Client) AgentRegister() (string, error) {
	// Create the agent registration
	var registration AgentRegistration
	registration.Name = MachineHostname()
	registration.Hostname = registration.Name

	// Register and return the agent
	err := c.Post(&registration, "/register", registration)
	if err != nil {
		return "", err
	}

	return registration.AccessToken, nil
}

func (a *AgentRegistration) String() string {
	return fmt.Sprintf("AgentRegistration{AccessToken: %s}", a.AccessToken)
}
