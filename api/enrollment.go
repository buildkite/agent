package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

// AgentEnrollmentResponse exchanges a single-use, key-bound enrollment grant
// for a credential that can register multiple workers on the same host.
// This experimental contract requires server support; existing servers reject it.
type AgentEnrollmentResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	DPoPJKT     string `json:"dpop_jkt"`
}

// Enroll redeems the configured grant once, before registering spawned workers.
// The server must verify the grant's key binding and consume it atomically.
func (c *Client) Enroll(ctx context.Context) (*AgentEnrollmentResponse, *Response, error) {
	if c.conf.HostKey == nil {
		return nil, nil, fmt.Errorf("enrollment requires a host key")
	}
	req, err := c.newRequest(ctx, http.MethodPost, "enroll", nil)
	if err != nil {
		return nil, nil, err
	}
	enrollment := new(AgentEnrollmentResponse)
	resp, err := c.doRequest(req, enrollment)
	if err != nil {
		return nil, resp, err
	}
	if err := c.validateHostBinding(enrollment.AccessToken, enrollment.TokenType, enrollment.DPoPJKT); err != nil {
		return nil, resp, fmt.Errorf("enrollment: %w", err)
	}
	return enrollment, resp, nil
}

func (c *Client) validateHostBinding(token, tokenType, jkt string) error {
	if c.conf.HostKey == nil {
		if strings.EqualFold(tokenType, "DPoP") || jkt != "" {
			return fmt.Errorf("server returned a bound credential without a configured host key")
		}
		return nil
	}
	if token == "" || !strings.EqualFold(tokenType, "DPoP") || jkt != c.conf.HostKey.Thumbprint() {
		return fmt.Errorf("server did not confirm the requested DPoP host-key binding")
	}
	return nil
}
