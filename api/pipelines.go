package api

import "fmt"

// PipelinesService handles communication with the pipeline related methods of the
// Buildkite Agent API.
type PipelinesService struct {
	client *Client
}

// Pipeline represents a Buildkite Agent API Pipeline
type Pipeline struct {
	UUID     string      `json:"uuid"`
	Pipeline interface{} `json:"pipeline"`
	Replace  bool        `json:"replace,omitempty"`
}

// Uploads the pipeline to the Buildkite Agent API. This request doesn't use JSON,
// but a multi-part HTTP form upload
func (cs *PipelinesService) Upload(jobId string, pipeline *Pipeline) (*Response, error) {
	u := fmt.Sprintf("jobs/%s/pipelines", jobId)

	req, err := cs.client.NewRequest("POST", u, pipeline)
	if err != nil {
		return nil, err
	}

	return cs.client.Do(req, nil)
}
