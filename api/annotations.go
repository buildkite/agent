package api

import (
	"context"
	"fmt"
)

// Annotation represents a Buildkite Agent API Annotation
type Annotation struct {
	Body    string `json:"body,omitempty"`
	Context string `json:"context,omitempty"`
	Style   string `json:"style,omitempty"`
	Append  bool   `json:"append,omitempty"`
}

// Annotate a build in the Buildkite UI
func (c *Client) Annotate(ctx context.Context, jobId string, annotation *Annotation) (*Response, error) {
	u := fmt.Sprintf("jobs/%s/annotations", railsPathEscape(jobId))

	req, err := c.newRequest(ctx, "POST", u, annotation)
	if err != nil {
		return nil, err
	}

	return c.doRequest(req, nil)
}

// Remove an annotation from a build
func (c *Client) AnnotationRemove(ctx context.Context, jobId string, context string) (*Response, error) {
	u := fmt.Sprintf("jobs/%s/annotations/%s", railsPathEscape(jobId), railsPathEscape(context))

	req, err := c.newRequest(ctx, "DELETE", u, nil)
	if err != nil {
		return nil, err
	}

	return c.doRequest(req, nil)
}
