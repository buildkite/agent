package api

import (
	"context"
	"fmt"
)

type OSCReport struct {
	State    string `json:"state"`
	ID       string `json:"id,omitempty"`
	App      string `json:"app,omitempty"`
	Kind     string `json:"kind,omitempty"`
	Title    string `json:"title,omitempty"`
	Msg      string `json:"msg,omitempty"`
	Progress *int   `json:"progress,omitempty"`
}

type OSCEvidence struct {
	Offset uint64    `json:"offset"`
	Report OSCReport `json:"report"`
}

// ForwardOSC sends decoded reports from the already-redacted job output.
// The server validates and selects evidence without classifying messages on the agent.
func (c *Client) ForwardOSC(ctx context.Context, id string, reports []OSCEvidence) (*Response, error) {
	req, err := c.newRequest(ctx, "POST", fmt.Sprintf("jobs/%s/osc_evidence", railsPathEscape(id)), map[string]any{"reports": reports})
	if err != nil {
		return nil, err
	}
	return c.doRequest(req, nil)
}
