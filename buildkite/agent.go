package buildkite

type Agent struct {
	API         API
	Name        string   `json:"name"`
	AccessToken string   `json:"access_token"`
	Hostname    string   `json:"hostname"`
	OS          string   `json:"os"`
	CommandEval bool     `json:"script_eval_enabled"`
	Priority    string   `json:"priority,omitempty"`
	Version     string   `json:"version"`
	MetaData    []string `json:"meta_data"`
	PID         int      `json:"pid,omitempty"`
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
