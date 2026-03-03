package kubernetes

import (
	"context"
	"errors"
	"fmt"
	"net/rpc"
	"time"

	"github.com/buildkite/roko"
)

// ErrInterruptBeforeStart is returned by StatusLoop when the job state goes
// directly from Wait to Interrupt (do not pass Go, do not collect $200).
var ErrInterruptBeforeStart = errors.New("job interrupted before starting")

type Client struct {
	ID         int
	SocketPath string

	client *rpc.Client
}

var errNotConnected = errors.New("client not connected")

// Connect establishes a connection to the Agent container in the same k8s pod and registers the client.
// Because k8s might run the containers "out of order", the server socket might not exist yet,
// so this method retries the connection with a 1-second interval until the context is cancelled.
// Callers should use context.WithTimeout to control the connection timeout.
func (c *Client) Connect(ctx context.Context) (*RegisterResponse, error) {
	if c.SocketPath == "" {
		c.SocketPath = defaultSocketPath
	}

	// Retry until the context is cancelled. The high maxAttempts is a safety net
	// in case the caller forgets to set a context deadline - in practice the
	// context deadline should be the limiting factor.
	const retryInterval = time.Second
	r := roko.NewRetrier(
		roko.WithMaxAttempts(3600),
		roko.WithStrategy(roko.Constant(retryInterval)),
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

// StatusLoop starts a goroutine that periodically pings the server for job
// status. It blocks until it is in the Start state, or it is interrupted early
// (with or without an error). After returning the goroutine continues pinging
// for status until the context is closed, calling onInterrupt when it becomes
// interrupted (with nil) or it encounters an error such as [rpc.ServerError].
func (c *Client) StatusLoop(ctx context.Context, onInterrupt func(error)) error {
	started := make(chan struct{})
	errs := make(chan error)
	interrupted := false

	report := func(err error) {
		if err == nil {
			// Only onInterrupt(nil) once.
			if interrupted {
				return
			}
			interrupted = true
		}
		select {
		case errs <- err:
			// still waiting to start
		default:
			// after waiting to start
			if onInterrupt != nil {
				onInterrupt(err)
			}
		}
	}

	go func() {
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
				report(nil)
				return

			case <-first:
				// continue below

			case <-ticker.C:
				// continue below
			}

			var current RunState
			if err := c.client.Call("Runner.Status", c.ID, &current); err != nil {
				report(err)
				return
			}
			switch current {
			case RunStateWait:
				// ask again later

			case RunStateStart:
				select {
				case <-started:
					// it was already closed
				default:
					close(started)
				}

			case RunStateInterrupt:
				report(nil)
			}
		}
	}()

	select {
	case <-started:
		return nil

	case err := <-errs:
		if err != nil {
			return fmt.Errorf("waiting for client to become ready: %w", err)
		}
		return ErrInterruptBeforeStart
	}
}

func (c *Client) Close() {
	c.client.Close() //nolint:errcheck // best-effort cleanup; rpc.Client.Close only closes the underlying conn
}
