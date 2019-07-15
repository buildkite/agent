package api

import "time"

// Heartbeat represents a Buildkite Agent API Heartbeat
type Heartbeat struct {
	SentAt     string `json:"sent_at"`
	ReceivedAt string `json:"received_at,omitempty"`
}

// Heartbeat notifies Buildkite that an agent is still connected
func (c *Client) Heartbeat() (*Heartbeat, *Response, error) {
	// Include the current time in the heartbeat, and include the operating
	// systems timezone.
	heartbeat := &Heartbeat{SentAt: time.Now().Format(time.RFC3339Nano)}

	req, err := c.newRequest("POST", "heartbeat", &heartbeat)
	if err != nil {
		return nil, nil, err
	}

	resp, err := c.doRequest(req, heartbeat)
	if err != nil {
		return nil, resp, err
	}

	return heartbeat, resp, err
}
