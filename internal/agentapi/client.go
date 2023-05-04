package agentapi

import (
	"context"
	"net/url"

	"github.com/buildkite/agent/v3/internal/socket"
)

const lockAPIPrefix = "http://agent/api/leader/v0/lock/"

// Client is a client for the leader API socket.
type Client struct {
	sc *socket.Client
}

// NewClient creates a new Client using the socket at a given path.
func NewClient(path string) (*Client, error) {
	// The API is unauthenticated, hence the empty token.
	sc, err := socket.NewClient(path, "")
	if err != nil {
		return nil, err
	}
	return &Client{sc: sc}, nil
}

// Get gets the current value of the lock key.
func (c *Client) Get(ctx context.Context, key string) (string, error) {
	uk := url.PathEscape(key)

	var resp ValueResponse
	if err := c.sc.Do(ctx, "GET", lockAPIPrefix+uk, nil, &resp); err != nil {
		return "", err
	}
	return resp.Value, nil
}

// CompareAndSwap atomically compares-and-swaps the old value for the new value
// or performs no modification. It returns the most up-to-date value for the
// key, and reports whether the new value was written.
func (c *Client) CompareAndSwap(ctx context.Context, key, old, new string) (string, bool, error) {
	uk := url.PathEscape(key)

	req := LockCASRequest{
		Old: old,
		New: new,
	}
	var resp LockCASResponse
	if err := c.sc.Do(ctx, "GET", lockAPIPrefix+uk, &req, &resp); err != nil {
		return "", false, err
	}
	return resp.Value, resp.Swapped, nil
}
