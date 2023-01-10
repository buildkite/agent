package api

import (
	"context"
	"fmt"
)

// Pipeline represents a Buildkite Agent API Pipeline
type Pipeline struct {
	UUID     string `json:"uuid"`
	Pipeline any    `json:"pipeline"`
	Replace  bool   `json:"replace,omitempty"`
}

// Uploads the pipeline to the Buildkite Agent API. This request doesn't use JSON,
// but a multi-part HTTP form upload
func (c *Client) UploadPipeline(ctx context.Context, jobId string, pipeline *Pipeline) (*Response, error) {
	u := fmt.Sprintf("jobs/%s/pipelines", jobId)

	req, err := c.newRequest(ctx, "POST", u, pipeline)
	if err != nil {
		return nil, err
	}

	return c.doRequest(req, nil)
}
