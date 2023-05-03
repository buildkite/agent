package agentapi

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/buildkite/agent/v3/internal/socket"
)

const lockAPIPrefix = "http://agent/api/leader/v0/lock/"

// Client is a client for the agent API socket.
type Client struct {
	sc *socket.Client
}

// NewClient creates a new Client using the socket at a given path. The context
// is used for an internal check that the socket can be dialled.
func NewClient(ctx context.Context, path string) (*Client, error) {
	// The API is unauthenticated, hence the empty token.
	sc, err := socket.NewClient(ctx, path, "")
	if err != nil {
		return nil, err
	}
	return &Client{sc: sc}, nil
}

// Ping pings the server. It returns a non-nil error if the ping fails, or the
// response timestamp is more than 100 milliseconds different to time.Now.
func (c *Client) Ping(ctx context.Context) error {
	var resp PingResponse
	if err := c.sc.Do(ctx, "GET", "http://agent/api/leader/v0/ping", nil, &resp); err != nil {
		return err
	}
	if time.Since(resp.Now) > 100*time.Millisecond {
		return fmt.Errorf("ping timestamp %v too old", resp.Now)
	}
	return nil
}

// LockGet gets the current value of the lock key.
func (c *Client) LockGet(ctx context.Context, key string) (string, error) {
	uk := "?key=" + url.QueryEscape(key)

	var resp ValueResponse
	if err := c.sc.Do(ctx, "GET", lockAPIPrefix+uk, nil, &resp); err != nil {
		return "", err
	}
	return resp.Value, nil
}

// LockCompareAndSwap atomically compares-and-swaps the old value for the new
// value, or performs no modification. It returns the most up-to-date value for
// the key, and reports whether the new value was written.
func (c *Client) LockCompareAndSwap(ctx context.Context, key, old, new string) (string, bool, error) {
	uk := "?key=" + url.QueryEscape(key)

	req := LockCASRequest{
		Old: old,
		New: new,
	}
	var resp LockCASResponse
	if err := c.sc.Do(ctx, "PATCH", lockAPIPrefix+uk, &req, &resp); err != nil {
		return "", false, err
	}
	return resp.Value, resp.Swapped, nil
}
