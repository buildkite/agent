package api

type AgentsService struct {
	client *Client
}

type Agent struct {
	Name              string   `json:"name"`
	AccessToken       string   `json:"access_token"`
	Hostname          string   `json:"hostname"`
	OS                string   `json:"os"`
	ScriptEvalEnabled bool     `json:"script_eval_enabled"`
	Priority          string   `json:"priority,omitempty"`
	Version           string   `json:"version"`
	MetaData          []string `json:"meta_data"`
	PID               int      `json:"pid,omitempty"`
}

func (as *AgentsService) Register(agent *Agent) (*Agent, *Response, error) {
	req, err := as.client.NewRequest("POST", "v2/register", agent)
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

func (as *AgentsService) Connect() (*Response, error) {
	req, err := as.client.NewRequest("POST", "v2/connect", nil)
	if err != nil {
		return nil, err
	}

	return as.client.Do(req, nil)
}

func (as *AgentsService) Disconnect() (*Response, error) {
	req, err := as.client.NewRequest("POST", "v2/disconnect", nil)
	if err != nil {
		return nil, err
	}

	return as.client.Do(req, nil)
}
