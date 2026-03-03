//go:build !windows

package kubernetes

import (
	"context"
	"encoding/gob"
	"errors"
	"net/rpc"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/buildkite/agent/v3/logger"
)

func TestOrderedClients(t *testing.T) {
	runner := newRunner(t, 3)
	socketPath := runner.conf.SocketPath

	clients := []*Client{{ID: 0}, {ID: 1}, {ID: 2}}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	for _, client := range clients {
		client.SocketPath = socketPath
		if err := connect(ctx, client); err != nil {
			t.Errorf("connect(ctx, client) error = %v", err)
		}
		t.Cleanup(client.Close)
	}

	for i := range clients {
		err := clients[i].StatusLoop(ctx, func(err error) {
			if err != nil {
				t.Errorf("clients[%d] interrupted after start: err = %v", i, err)
			}
		})
		if err != nil {
			t.Errorf("clients[%d].StatusLoop(ctx, onInterrupt) = %v", i, err)
		}
		if err := clients[i].Exit(0); err != nil {
			t.Errorf("clients[%d].Exit(0) = %v", i, err)
		}
	}

	select {
	case <-runner.Done():
		break
	default:
		t.Fatal("runner should be done when all clients have exited")
	}
}

func TestLivenessCheck(t *testing.T) {
	runner := newRunner(t, 2)
	socketPath := runner.conf.SocketPath

	clients := []*Client{{ID: 0}, {ID: 1}}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)

	for _, client := range clients {
		client.SocketPath = socketPath
		if err := connect(ctx, client); err != nil {
			t.Errorf("connect(ctx, client) error = %v", err)
		}
		t.Cleanup(client.Close)
	}

	interrupted := make(chan struct{})

	// Only start the ping loop for client 0.
	// Client 1 should time out after 2 seconds.
	err := clients[0].StatusLoop(ctx, func(error) {
		// Don't care what the err is here - just that client0 is interrupted.
		select {
		case <-interrupted:
			// already closed
		default:
			close(interrupted)
		}
	})
	if err != nil {
		t.Errorf("clients[%d].StatusLoop(ctx, onInterrupt) = %v", 0, err)
	}

	select {
	case <-runner.Done():
		break
	case <-ctx.Done():
		t.Errorf("timed out waiting for clients[1] to be declared lost and job terminated")
	}

	<-interrupted
}

func TestDuplicateClients(t *testing.T) {
	runner := newRunner(t, 2)
	socketPath := runner.conf.SocketPath

	client0 := &Client{ID: 0, SocketPath: socketPath}
	client1 := &Client{ID: 0, SocketPath: socketPath}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	if err := connect(ctx, client0); err != nil {
		t.Errorf("connect(ctx, client0) error = %v", err)
	}
	if err := connect(ctx, client1); err == nil {
		t.Errorf("connect(ctx, client1) error = %v, want some error (connecting a client with a duplicate ID)", err)
	}
}

func TestExcessClients(t *testing.T) {
	runner := newRunner(t, 1)
	socketPath := runner.conf.SocketPath

	client0 := &Client{ID: 0, SocketPath: socketPath}
	client1 := &Client{ID: 1, SocketPath: socketPath}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	if err := connect(ctx, client0); err != nil {
		t.Errorf("connect(ctx, client0) error = %v", err)
	}
	if err := connect(ctx, client1); err == nil {
		t.Errorf("connect(ctx, client1) error = %v, want some error (connecting too many clients)", err)
	}
}

func TestWaitStatusNonZero(t *testing.T) {
	runner := newRunner(t, 2)

	client0 := &Client{ID: 0, SocketPath: runner.conf.SocketPath}
	client1 := &Client{ID: 1, SocketPath: runner.conf.SocketPath}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	if err := connect(ctx, client0); err != nil {
		t.Errorf("connect(ctx, client0) error = %v", err)
	}
	if err := connect(ctx, client1); err != nil {
		t.Errorf("connect(ctx, client1) error = %v", err)
	}
	if err := client0.Exit(1); err != nil {
		t.Errorf("client0.Exit(1) error = %v", err)
	}
	if err := client1.Exit(0); err != nil {
		t.Errorf("client1.Exit(0) error = %v", err)
	}
	if got, want := runner.WaitStatus().ExitStatus(), 1; got != want {
		t.Errorf("runner.WaitStatus().ExitStatus() = %d, want %d", got, want)
	}
}

func TestInterruptBeforeStart(t *testing.T) {
	runner := newRunner(t, 2)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	client0 := &Client{ID: 0, SocketPath: runner.conf.SocketPath}

	if err := connect(ctx, client0); err != nil {
		t.Errorf("connect(ctx, client0) error = %v", err)
	}
	if err := runner.Interrupt(); err != nil {
		t.Errorf("runner.Interrupt() error = %v", err)
	}

	err := client0.StatusLoop(ctx, func(err error) {
		if err != nil {
			t.Errorf("client0 interrupted after start: err = %v", err)
		}
	})
	if !errors.Is(err, ErrInterruptBeforeStart) {
		t.Errorf("client0.StatusLoop(ctx, onInterrupt) = %v, want %v", err, ErrInterruptBeforeStart)
	}
}

func TestTerminateBeforeStart(t *testing.T) {
	runner := newRunner(t, 2)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	client0 := &Client{ID: 0, SocketPath: runner.conf.SocketPath}

	if err := connect(ctx, client0); err != nil {
		t.Errorf("connect(ctx, client0) error = %v", err)
	}
	if err := runner.Terminate(); err != nil {
		t.Errorf("runner.Terminate() error = %v", err)
	}

	err := client0.StatusLoop(ctx, func(err error) {
		if err != nil {
			t.Errorf("client0 interrupted after start: err = %v", err)
		}
	})
	if wantErr := rpc.ServerError(rpc.ErrShutdown.Error()); !errors.Is(err, wantErr) {
		t.Errorf("client0.StatusLoop(ctx, onInterrupt) = %[1]T(%[1]v), want %[2]T(%[2]v)", err, wantErr)
	}
}

func TestInterruptAfterStart(t *testing.T) {
	runner := newRunner(t, 2)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	client0 := &Client{ID: 0, SocketPath: runner.conf.SocketPath}

	if err := connect(ctx, client0); err != nil {
		t.Errorf("connect(ctx, client0) error = %v", err)
	}

	interrupted := make(chan struct{})
	err := client0.StatusLoop(ctx, func(err error) {
		if err != nil {
			t.Errorf("client0 interrupted after start: err = %v", err)
			return
		}
		close(interrupted)
	})
	if err != nil {
		t.Errorf("client0.StatusLoop(ctx, onInterrupt) = %v", err)
	}

	if err := runner.Interrupt(); err != nil {
		t.Errorf("runner.Interrupt() error = %v", err)
	}

	<-interrupted
}

func TestTerminateAfterStart(t *testing.T) {
	runner := newRunner(t, 2)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	client0 := &Client{ID: 0, SocketPath: runner.conf.SocketPath}

	if err := connect(ctx, client0); err != nil {
		t.Errorf("connect(ctx, client0) error = %v", err)
	}

	terminated := make(chan struct{})
	err := client0.StatusLoop(ctx, func(err error) {
		wantErr := rpc.ServerError(rpc.ErrShutdown.Error())
		if errors.Is(err, wantErr) {
			close(terminated)
			return
		}
		if err != nil {
			t.Errorf("client0 interrupted after start: err = %[1]T(%[1]v), want %[2]T(%[2]v)", err, wantErr)
		}
	})
	if err != nil {
		t.Errorf("client0.StatusLoop(ctx, onInterrupt) = %v", err)
	}

	if err := runner.Terminate(); err != nil {
		t.Errorf("runner.Terminate() error = %v", err)
	}

	<-terminated
}

func newRunner(t *testing.T, clientCount int) *Runner {
	tempDir, err := os.MkdirTemp("", t.Name())
	if err := err; err != nil {
		t.Errorf("err error = %v", err)
	}
	socketPath := filepath.Join(tempDir, "bk.sock")
	t.Cleanup(func() {
		os.RemoveAll(tempDir) //nolint:errcheck // best-effort cleanup in test
	})
	runner := NewRunner(logger.Discard, RunnerConfig{
		SocketPath:         socketPath,
		ClientCount:        clientCount,
		ClientStartTimeout: 10 * time.Minute,
		ClientLostTimeout:  2 * time.Second,
	})
	runnerCtx, cancelRunner := context.WithCancel(context.Background())
	go runner.Run(runnerCtx) //nolint:errcheck // test goroutine; errors checked via test assertions
	t.Cleanup(cancelRunner)

	// wait for runner to listen
	timeout := time.After(10 * time.Second)
	check := time.Tick(time.Millisecond)
	var lastErr error
	for {
		select {
		case <-check:
			_, lastErr = os.Lstat(socketPath)
			if lastErr == nil {
				return runner
			}
		case <-timeout:
			t.Errorf("after 10 seconds, os.Lstat(%q) error = %v", socketPath, lastErr)
		}
	}
}

func init() {
	gob.Register(new(waitStatus))
}

// helper for ignoring the response from regular client.Connect
func connect(ctx context.Context, c *Client) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	_, err := c.Connect(ctx)
	return err
}
