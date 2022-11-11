package api

import (
	"fmt"
)

type OidcToken struct {
	Token string `json:"token"`
}

type OidcTokenRequest struct {
	JobId    string
	Audience string
}

func (c *Client) OidcToken(methodReq *OidcTokenRequest) (*OidcToken, *Response, error) {
	m := &struct {
		Audience string `json:"audience,omitempty"`
	}{
		Audience: methodReq.Audience,
	}

	u := fmt.Sprintf("jobs/%s/oidc/tokens", methodReq.JobId)
	httpReq, err := c.newRequest("POST", u, m)
	if err != nil {
		return nil, nil, err
	}

	t := &OidcToken{}
	resp, err := c.doRequest(httpReq, t)
	return t, resp, err
}
