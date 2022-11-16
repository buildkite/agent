package api

import "context"

// Ping represents a Buildkite Agent API Ping
type Ping struct {
	Action   string `json:"action,omitempty"`
	Message  string `json:"message,omitempty"`
	Job      *Job   `json:"job,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`
}

// Pings the API and returns any work the client needs to perform
func (c *Client) Ping(ctx context.Context) (*Ping, *Response, error) {
	req, err := c.newRequest(ctx, "GET", "ping", nil)
	if err != nil {
		return nil, nil, err
	}

	ping := new(Ping)
	resp, err := c.doRequest(req, ping)
	if err != nil {
		return nil, resp, err
	}

	return ping, resp, err
}
