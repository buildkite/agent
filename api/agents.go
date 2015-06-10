package api

// AgentsService handles communication with the agent related methods of the
// Buildkite Agent API.
type AgentsService struct {
	client *Client
}

// Agent represents an agent on the Buildkite Agent API
type Agent struct {
	Name              string   `json:"name"`
	AccessToken       string   `json:"access_token"`
	Hostname          string   `json:"hostname"`
	Endpoint          string   `json:"endpoint"`
	OS                string   `json:"os"`
	ScriptEvalEnabled bool     `json:"script_eval_enabled"`
	Priority          string   `json:"priority,omitempty"`
	Version           string   `json:"version"`
	MetaData          []string `json:"meta_data"`
	PID               int      `json:"pid,omitempty"`
}

// Registers the agent against the Buildktie Agent API. The client for this
// call must be authenticated using an Agent Registration Token
func (as *AgentsService) Register(agent *Agent) (*Agent, *Response, error) {
	req, err := as.client.NewRequest("POST", "register", agent)
	if err != nil {
		return nil, nil, err
	}

	a := new(Agent)
	resp, err := as.client.Do(req, a)
	if err != nil {
		return nil, resp, err
	}

	return a, resp, err
}

// Connects the agent to the Buildkite Agent API
func (as *AgentsService) Connect() (*Response, error) {
	req, err := as.client.NewRequest("POST", "connect", nil)
	if err != nil {
		return nil, err
	}

	return as.client.Do(req, nil)
}

// Disconnects the agent to the Buildkite Agent API
func (as *AgentsService) Disconnect() (*Response, error) {
	req, err := as.client.NewRequest("POST", "disconnect", nil)
	if err != nil {
		return nil, err
	}

	return as.client.Do(req, nil)
}
