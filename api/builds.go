package api

import (
	"context"
	"fmt"
)

type Build struct {
	ID string `json:"id"`
}

// CancelBuild cancels a build with the given ID
func (c *Client) CancelBuild(ctx context.Context, id string) (*Build, *Response, error) {
	u := fmt.Sprintf("builds/%s/cancel", railsPathEscape(id))

	req, err := c.newRequest(ctx, "POST", u, nil)
	if err != nil {
		return nil, nil, err
	}

	build := new(Build)
	resp, err := c.doRequest(req, build)
	if err != nil {
		return nil, resp, err
	}

	return build, resp, nil
}
