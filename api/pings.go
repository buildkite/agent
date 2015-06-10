package api

// PingsService handles communication with the ping related methods of the
// Buildkite Agent API.
type PingsService struct {
	client *Client
}

// Ping represents a Buildkite Agent API Ping
type Ping struct {
	Action   string `json:"action,omitempty"`
	Message  string `json:"message,omitempty"`
	Job      *Job   `json:"job,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`
}

// Pings the API and returns any work the client needs to perform
func (ps *PingsService) Get() (*Ping, *Response, error) {
	req, err := ps.client.NewRequest("GET", "ping", nil)
	if err != nil {
		return nil, nil, err
	}

	ping := new(Ping)
	resp, err := ps.client.Do(req, ping)
	if err != nil {
		return nil, resp, err
	}

	return ping, resp, err
}
