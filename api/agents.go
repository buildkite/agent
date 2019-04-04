package api

import (
	"net/http"

	"github.com/buildkite/agent/experiments"
)

// AgentsService handles communication with the agent related methods of the
// Buildkite Agent API.
type AgentsService struct {
	client *Client
}

// AgentRegisterRequest is a call to register on the Buildkite Agent API
type AgentRegisterRequest struct {
	Name              string   `json:"name" msgpack:"name"`
	Hostname          string   `json:"hostname" msgpack:"hostname"`
	OS                string   `json:"os" msgpack:"os"`
	Arch              string   `json:"arch" msgpack:"arch"`
	ScriptEvalEnabled bool     `json:"script_eval_enabled" msgpack:"script_eval_enabled"`
	Priority          string   `json:"priority,omitempty" msgpack:"priority,omitempty"`
	Version           string   `json:"version" msgpack:"version"`
	Build             string   `json:"build" msgpack:"build"`
	Tags              []string `json:"meta_data" msgpack:"meta_data"`
	PID               int      `json:"pid,omitempty" msgpack:"pid,omitempty"`
	MachineID         string   `json:"machine_id,omitempty" msgpack:"machine_id,omitempty"`
}

// AgentRegisterResponse is the response from the Buildkite Agent API
type AgentRegisterResponse struct {
	UUID              string   `json:"uuid" msgpack:"uuid"`
	Name              string   `json:"name" msgpack:"name"`
	AccessToken       string   `json:"access_token" msgpack:"access_token"`
	Endpoint          string   `json:"endpoint" msgpack:"endpoint"`
	PingInterval      int      `json:"ping_interval" msgpack:"ping_interval"`
	JobStatusInterval int      `json:"job_status_interval" msgpack:"job_status_interval"`
	HeartbeatInterval int      `json:"heartbeat_interval" msgpack:"heartbeat_interval"`
	Tags              []string `json:"meta_data" msgpack:"meta_data"`
}

// Registers the agent against the Buildkite Agent API. The client for this
// call must be authenticated using an Agent Registration Token
func (as *AgentsService) Register(regReq *AgentRegisterRequest) (*AgentRegisterResponse, *Response, error) {
	var req *http.Request
	var err error
	if experiments.IsEnabled("msgpack") {
		req, err = as.client.NewRequestWithMessagePack("POST", "register", regReq)
	} else {
		req, err = as.client.NewRequest("POST", "register", regReq)
	}

	if err != nil {
		return nil, nil, err
	}

	a := new(AgentRegisterResponse)
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
