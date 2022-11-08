package api

import (
	"encoding/json"
	"fmt"
)

type OidcTokenRequest struct {
	Audience string `json:"audience"`
}

type OidcToken struct {
	Token string `json:"token"`
}

func (c *Client) OidcToken(jobId, audience string) (*OidcToken, *Response, error) {
	u := fmt.Sprintf("jobs/%s/oidc/tokens", jobId)
	m := &OidcTokenRequest{Audience: audience}
	req, err := c.newRequest("POST", u, m)
	if err != nil {
		return nil, nil, err
	}

	resp, err := c.doRequest(req, m)
	if err != nil {
		return nil, nil, err
	}

	t := &OidcToken{}
	if err := json.NewDecoder(resp.Body).Decode(t); err != nil {
		return nil, resp, err
	}

	return t, resp, err
}
