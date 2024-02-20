package api

import (
	"context"
	"path"
)

type GetSecretRequest struct {
	Key   string
	JobID string
}

type Secret struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	UUID  string `json:"uuid"`
}

func (c *Client) GetSecret(ctx context.Context, req *GetSecretRequest) (*Secret, *Response, error) {
	httpReq, err := c.newRequest(ctx, "GET", path.Join("jobs", req.JobID, "secrets"), nil)
	if err != nil {
		return nil, nil, err
	}

	q := httpReq.URL.Query()
	q.Add("key", req.Key)
	httpReq.URL.RawQuery = q.Encode()

	secret := &Secret{}
	resp, err := c.doRequest(httpReq, secret)
	if err != nil {
		return nil, resp, err
	}

	return secret, resp, nil
}
