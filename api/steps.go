package api

import (
	"fmt"
)

// StepGetRequest represents a request for information about a step
type StepGetRequest struct {
	Attribute string `json:"attribute,omitempty"`
	Build     string `json:"build_id,omitempty"`
	Format    string `json:"format,omitempty"`
}

type StepGetResponse struct {
	Value string `json:"value"`
}

// StepGet gets an attribute from step
func (c *Client) StepGet(stepIdOrKey string, stepGetRequest *StepGetRequest) (*StepGetResponse, *Response, error) {
	u := fmt.Sprintf("steps/%s/get", stepIdOrKey)

	req, err := c.newRequest("POST", u, stepGetRequest)
	if err != nil {
		return nil, nil, err
	}

	r := new(StepGetResponse)
	resp, err := c.doRequest(req, r)
	if err != nil {
		return nil, resp, err
	}

	return r, resp, err
}

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
