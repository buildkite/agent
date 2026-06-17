package jobapi

import (
	"context"
	"errors"
	"net/http"
	"os"

	"github.com/buildkite/agent/v3/internal/socket"
)

const (
	envURL            = "http://job/api/current-job/v0/env"
	workdirURL        = "http://job/api/current-job/v0/workdir"
	redactionsURL     = "http://job/api/current-job/v0/redactions"
	promiseFailureURL = "http://job/api/current-job/v0/promise-failure"
)

var (
	// ErrJobAPIUnavailable is returned when the current machine cannot support the Job API.
	ErrJobAPIUnavailable = errors.New("job API is unavailable on this machine")
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
	if !socket.Available() {
		return "", "", ErrJobAPIUnavailable
	}

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

// SetWorkdir requests that subsequent hooks and the command phase run in dir.
// dir must be an absolute path. It returns the absolute working directory as
// recorded by the executor.
func (c *Client) SetWorkdir(ctx context.Context, dir string) (string, error) {
	req := WorkdirSetRequest{Workdir: dir}
	var resp WorkdirSetResponse
	if err := c.client.Do(ctx, http.MethodPut, workdirURL, &req, &resp); err != nil {
		return "", err
	}
	return resp.Workdir, nil
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

// DeclarePromiseFailure asks the Job API to declare a promised failure with the
// given exit status and reason to the Buildkite API, blocking until it
// completes. The server debounces repeated and concurrent calls for the same
// exit status: concurrent callers share one in-flight call, and once it succeeds
// later calls return from the cache. A nil error means the promise was accepted.
// A failed declaration is returned as a socket.APIErr carrying the HTTP status
// code (the Buildkite API's status when it responded, otherwise 502).
func (c *Client) DeclarePromiseFailure(ctx context.Context, exitStatus int, reason string) error {
	req := PromiseFailureRequest{
		ExitStatus: exitStatus,
		Reason:     reason,
	}
	return c.client.Do(ctx, http.MethodPost, promiseFailureURL, &req, nil)
}
