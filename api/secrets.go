package api

import (
	"context"
	"path"
)

// GetSecretRequest represents a request to read a secret from the Buildkite Agent API.
type GetSecretRequest struct {
	Key   string
	JobID string
}

// Secret represents a secret read from the Buildkite Agent API.
type Secret struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	UUID  string `json:"uuid"`
}

// GetSecretRequest represents a request to read multiple secrets from the Buildkite Agent API.
type GetSecretsRequest struct {
	Keys  []string
	JobID string
}

// GetSecretsResponse represents the response when reading multiple secrets from the Buildkite Agent API.
type GetSecretsResponse struct {
	Secrets []Secret `json:"secrets"`
}

// GetSecret reads a secret from the Buildkite Agent API.
func (c *Client) GetSecret(ctx context.Context, req *GetSecretRequest) (*Secret, *Response, error) {
	// the endpoint is /jobs/:job_id/secrets?key=:key
	httpReq, err := c.newRequest(ctx, "GET", path.Join("jobs", railsPathEscape(req.JobID), "secrets"), nil)
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

// GetSecrets reads multiple secrets from the Buildkite Agent API.
func (c *Client) GetSecrets(ctx context.Context, req *GetSecretsRequest) (*GetSecretsResponse, *Response, error) {
	// the endpoint is /jobs/:job_id/secrets?key[]=:key1&key[]=:key2
	httpReq, err := c.newRequest(ctx, "GET", path.Join("jobs", railsPathEscape(req.JobID), "secrets"), nil)
	if err != nil {
		return nil, nil, err
	}

	q := httpReq.URL.Query()
	for _, key := range req.Keys {
		q.Add("key[]", key)
	}
	httpReq.URL.RawQuery = q.Encode()

	secretsResp := &GetSecretsResponse{}
	resp, err := c.doRequest(httpReq, secretsResp)
	if err != nil {
		return nil, resp, err
	}

	return secretsResp, resp, nil
}
