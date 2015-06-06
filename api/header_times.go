package api

import (
	"fmt"
)

// HeaderTimesService handles communication with the meta data related methods
// of the Buildkite Agent API.
type HeaderTimesService struct {
	client *Client
}

// HeaderTimes represents a set of header times that are associated with a job
// log.
type HeaderTimes struct {
	Times map[string]string `json:"header_times"`
}

// Saves the header times to the job
func (hs *HeaderTimesService) Save(jobId string, headerTimes *HeaderTimes) (*Response, error) {
	u := fmt.Sprintf("v2/jobs/%s/header_times", jobId)

	req, err := hs.client.NewRequest("POST", u, headerTimes)
	if err != nil {
		return nil, err
	}

	resp, err := hs.client.Do(req, nil)
	if err != nil {
		return resp, err
	}

	return resp, err
}
