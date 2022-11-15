package api

import (
	"fmt"
)

type OIDCToken struct {
	Token string `json:"token"`
}

type OIDCTokenRequest struct {
	Job      string
	Audience string
	Lifetime int
}

func (c *Client) OIDCToken(methodReq *OIDCTokenRequest) (*OIDCToken, *Response, error) {
	m := &struct {
		Audience string `json:"audience,omitempty"`
		Lifetime int    `json:"lifetime,omitempty"`
	}{
		Audience: methodReq.Audience,
		Lifetime: methodReq.Lifetime,
	}

	u := fmt.Sprintf("jobs/%s/oidc/tokens", methodReq.Job)
	httpReq, err := c.newRequest("POST", u, m)
	if err != nil {
		return nil, nil, err
	}

	t := &OIDCToken{}
	resp, err := c.doRequest(httpReq, t)
	if err != nil {
		return nil, resp, err
	}

	return t, resp, nil
}
