package api

import (
	"fmt"
)

// StepUpdate represents a change request to a step
type StepUpdate struct {
	IdempotencyUUID string `json:"idempotency_uuid,omitempty"`
	Build           string `json:"build_id,omitempty"`
	Attribute       string `json:"attribute,omitempty"`
	Value           string `json:"value,omitempty"`
	Append          bool   `json:"append,omitempty"`
}

// StepUpdate updates a step
func (c *Client) StepUpdate(stepIdOrKey string, stepUpdate *StepUpdate) (*Response, error) {
	u := fmt.Sprintf("steps/%s", stepIdOrKey)

	req, err := c.newRequest("PUT", u, stepUpdate)
	if err != nil {
		return nil, err
	}

	return c.doRequest(req, nil)
}
