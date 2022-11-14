package api

import (
	"fmt"
)

type OIDCToken struct {
	Token string `json:"token"`
}

type OIDCTokenRequest struct {
	JobId    string
	Audience string
}

func (c *Client) OIDCToken(methodReq *OIDCTokenRequest) (*OIDCToken, *Response, error) {
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

	t := &OIDCToken{}
	resp, err := c.doRequest(httpReq, t)
	return t, resp, err
}
