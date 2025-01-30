package kubernetes

import (
	"context"
	"errors"
	"net/rpc"
	"time"

	"github.com/buildkite/roko"
)

type Client struct {
	ID         int
	SocketPath string

	client *rpc.Client
}

var errNotConnected = errors.New("client not connected")

func (c *Client) Connect(ctx context.Context) (*RegisterResponse, error) {
	if c.SocketPath == "" {
		c.SocketPath = defaultSocketPath
	}

	// Because k8s might run the containers "out of order", the server socket
	// might not exist yet. Try to connect several times.
	r := roko.NewRetrier(
		roko.WithMaxAttempts(30),
		roko.WithStrategy(roko.Constant(time.Second)),
	)
	client, err := roko.DoFunc(ctx, r, func(*roko.Retrier) (*rpc.Client, error) {
		return rpc.DialHTTP("unix", c.SocketPath)
	})
	if err != nil {
		return nil, err
	}
	c.client = client
	var resp RegisterResponse
	if err := c.client.Call("Runner.Register", c.ID, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) Exit(exitStatus int) error {
	if c.client == nil {
		return errNotConnected
	}
	return c.client.Call("Runner.Exit", ExitCode{
		ID:         c.ID,
		ExitStatus: exitStatus,
	}, nil)
}

// Write implements io.Writer
func (c *Client) Write(p []byte) (int, error) {
	if c.client == nil {
		return 0, errNotConnected
	}
	n := len(p)
	err := c.client.Call("Runner.WriteLogs", Logs{
		Data: p,
	}, nil)
	return n, err
}

var ErrInterrupt = errors.New("interrupt signal received")

func (c *Client) Await(ctx context.Context, desiredState RunState) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	// Because [time.Ticker] doesn't tick until after the duration (time.Second),
	// but we want to call the RPC method in the first loop iteration without
	// waiting, here's a channel (first) that can be received from once and then
	// never again, providing a non-blocking path through the select on the
	// first iteration.
	first := make(chan struct{}, 1)
	first <- struct{}{}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-first:
			// continue below

		case <-ticker.C:
			// continue below
		}

		var current RunState
		if err := c.client.Call("Runner.Status", c.ID, &current); err != nil {
			return err
		}
		if current == desiredState {
			return nil
		}
		if current == RunStateInterrupt {
			return ErrInterrupt
		}
	}
}

func (c *Client) Close() {
	c.client.Close()
}
