package api

import (
	"fmt"
)

// StepsService handles communication with the step related methods of the
// Buildkite Agent API.
type StepsService struct {
	client *Client
}

// StepUpdate represents a change request to a step
type StepUpdate struct {
	UUID      string `json:"uuid,omitempty"`
	Attribute string `json:"attribute,omitempty"`
	Value     string `json:"value,omitempty"`
	Append    bool   `json:"append,omitempty"`
}

// Updates a step
func (js *StepsService) Update(stepId string, stepUpdate *StepUpdate) (*Response, error) {
	u := fmt.Sprintf("steps/%s", stepId)

	req, err := js.client.NewRequest("PUT", u, stepUpdate)
	if err != nil {
		return nil, err
	}

	return js.client.Do(req, nil)
}
