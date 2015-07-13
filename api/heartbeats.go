package api

import "time"

// HeartbeatsService handles communication with the ping related methods of the
// Buildkite Agent API.
type HeartbeatsService struct {
	client *Client
}

// Heartbeat represents a Buildkite Agent API Heartbeat
type Heartbeat struct {
	SentAt     string `json:"sent_at"`
	ReceivedAt string `json:"received_at,omitempty"`
}

// Heartbeats the API which keeps the agent connected to Buildkite
func (hs *HeartbeatsService) Beat() (*Heartbeat, *Response, error) {
	// Include the current time in the heartbeat, and include the operating
	// systems timezone.
	heartbeat := &Heartbeat{SentAt: time.Now().Format(time.RFC3339Nano)}

	req, err := hs.client.NewRequest("POST", "heartbeat", &heartbeat)
	if err != nil {
		return nil, nil, err
	}

	resp, err := hs.client.Do(req, heartbeat)
	if err != nil {
		return nil, resp, err
	}

	return heartbeat, resp, err
}
