package jobapi

import (
	"context"
	"errors"
	"net/http"
	"os"

	"github.com/buildkite/agent/v3/internal/socket"
)

const (
	envURL        = "http://job/api/current-job/v0/env"
	redactionsURL = "http://job/api/current-job/v0/redactions"
)

var (
	errNoJobAPISocketEnv = errors.New("BUILDKITE_AGENT_JOB_API_SOCKET empty or undefined")
	errNoJobAPITokenEnv  = errors.New("BUILDKITE_AGENT_JOB_API_TOKEN empty or undefined")
)

// Client connects to the Job API.
type Client struct {
	client *socket.Client
}

// NewDefaultClient returns a new Job API Client with the default socket path
// and token.
func NewDefaultClient(ctx context.Context) (*Client, error) {
	sock, token, err := DefaultSocketPath()
	if err != nil {
		return nil, err
	}

	return NewClient(ctx, sock, token)
}

// DefaultSocketPath returns the socket path and access token, if available.
func DefaultSocketPath() (path, token string, err error) {
	path = os.Getenv("BUILDKITE_AGENT_JOB_API_SOCKET")
	if path == "" {
		return "", "", errNoJobAPISocketEnv
	}

	token = os.Getenv("BUILDKITE_AGENT_JOB_API_TOKEN")
	if token == "" {
		return "", "", errNoJobAPITokenEnv
	}

	return path, token, nil
}

// NewClient creates a new Job API Client.
func NewClient(ctx context.Context, sock, token string) (*Client, error) {
	cli, err := socket.NewClient(ctx, sock, token)
	if err != nil {
		return nil, err
	}
	return &Client{client: cli}, nil
}

// EnvGet gets the current environment variables from within the job executor.
func (c *Client) EnvGet(ctx context.Context) (map[string]string, error) {
	var resp EnvGetResponse
	if err := c.client.Do(ctx, "GET", envURL, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Env, nil
}

// EnvUpdate updates environment variables within the job executor.
func (c *Client) EnvUpdate(ctx context.Context, req *EnvUpdateRequest) (*EnvUpdateResponse, error) {
	var resp EnvUpdateResponse
	if err := c.client.Do(ctx, "PATCH", envURL, req, &resp); err != nil {
		return nil, err
	}
	resp.Normalize()
	return &resp, nil
}

// EnvDelete deletes environment variables within the job executor.
func (c *Client) EnvDelete(ctx context.Context, del []string) (deleted []string, err error) {
	req := EnvDeleteRequest{
		Keys: del,
	}
	var resp EnvDeleteResponse
	if err := c.client.Do(ctx, "DELETE", envURL, &req, &resp); err != nil {
		return nil, err
	}
	resp.Normalize()
	return resp.Deleted, nil
}

// RedactionCreate creates a redaction in the job executor.
func (c *Client) RedactionCreate(ctx context.Context, text string) (string, error) {
	req := RedactionCreateRequest{
		Redact: text,
	}
	var resp RedactionCreateResponse
	if err := c.client.Do(ctx, http.MethodPost, redactionsURL, &req, &resp); err != nil {
		return "", err
	}
	return resp.Redacted, nil
}
