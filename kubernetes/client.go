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
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
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
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Second):
			}
		}
	}
}

func (c *Client) Close() {
	c.client.Close()
}
