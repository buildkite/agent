package api

import (
	"context"
	"fmt"
)

type SecretGetRequest struct {
	Name string
}

type Secret struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func (c *Client) GetSecret(ctx context.Context, req *SecretGetRequest) (*Secret, *Response, error) {
	u := fmt.Sprintf("secrets/%s", railsPathEscape(req.Name))
	httpReq, err := c.newRequest(ctx, "GET", u, nil)
	if err != nil {
		return nil, nil, err
	}

	secret := &Secret{}
	resp, err := c.doRequest(httpReq, secret)
	if err != nil {
		return nil, resp, err
	}

	return secret, resp, nil
}
