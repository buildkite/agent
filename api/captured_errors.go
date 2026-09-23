package api

import (
	"context"
	"fmt"
	"time"
)

// JobCapturedError is a validated error reported by a running job.
type JobCapturedError struct {
	Code           string         `json:"code"`
	Message        string         `json:"message"`
	Timestamp      time.Time      `json:"timestamp"`
	IdempotencyKey string         `json:"idempotency_key,omitempty"`
	Context        map[string]any `json:"context,omitempty"`
}

// CaptureJobError submits a structured error using the current job's token.
// Reporting is best-effort: this method makes a single attempt.
func (c *Client) CaptureJobError(ctx context.Context, id string, capturedError *JobCapturedError) (*Response, error) {
	req, err := c.newRequest(ctx, "POST", fmt.Sprintf("jobs/%s/errors", railsPathEscape(id)), capturedError)
	if err != nil {
		return nil, err
	}
	return c.doRequest(req, nil)
}
