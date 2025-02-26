//go:build !windows

package kubernetes

import (
	"context"
	"encoding/gob"
	"net/rpc"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/buildkite/agent/v3/logger"
	"github.com/stretchr/testify/require"
)

func TestOrderedClients(t *testing.T) {
	runner := newRunner(t, 3)
	socketPath := runner.conf.SocketPath

	client0 := &Client{ID: 0}
	client1 := &Client{ID: 1}
	client2 := &Client{ID: 2}
	clients := []*Client{client0, client1, client2}

	for _, client := range clients {
		client.SocketPath = socketPath
		require.NoError(t, connect(client))
		t.Cleanup(client.Close)
	}
	ctx := context.Background()
	require.NoError(t, client0.Await(ctx, RunStateStart))
	require.NoError(t, client1.Await(ctx, RunStateWait))
	require.NoError(t, client2.Await(ctx, RunStateWait))

	require.NoError(t, client0.Exit(0))
	require.NoError(t, client0.Await(ctx, RunStateStart))
	require.NoError(t, client1.Await(ctx, RunStateStart))
	require.NoError(t, client2.Await(ctx, RunStateWait))

	require.NoError(t, client1.Exit(0))
	require.NoError(t, client0.Await(ctx, RunStateStart))
	require.NoError(t, client1.Await(ctx, RunStateStart))
	require.NoError(t, client2.Await(ctx, RunStateStart))

	require.NoError(t, client2.Exit(0))
	select {
	case <-runner.Done():
		break
	default:
		require.FailNow(t, "runner should be done when all clients have exited")
	}
}

func TestLivenessCheck(t *testing.T) {
	runner := newRunner(t, 2)
	socketPath := runner.conf.SocketPath

	client0 := &Client{ID: 0}
	client1 := &Client{ID: 1}
	clients := []*Client{client0, client1}

	for _, client := range clients {
		client.SocketPath = socketPath
		require.NoError(t, connect(client))
		t.Cleanup(client.Close)
	}
	ctx := context.Background()
	require.NoError(t, client0.Await(ctx, RunStateStart))
	require.NoError(t, client1.Await(ctx, RunStateWait))

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	select {
	case <-runner.Done():
		break
	case <-ctx.Done():
		t.Fatalf("timed out waiting for client0 to be declared lost and job terminated")
	}
}

func TestDuplicateClients(t *testing.T) {
	runner := newRunner(t, 2)
	socketPath := runner.conf.SocketPath

	client0 := &Client{ID: 0, SocketPath: socketPath}
	client1 := &Client{ID: 0, SocketPath: socketPath}

	require.NoError(t, connect(client0))
	require.Error(t, connect(client1), "expected an error when connecting a client with a duplicate ID")
}

func TestExcessClients(t *testing.T) {
	runner := newRunner(t, 1)
	socketPath := runner.conf.SocketPath

	client0 := &Client{ID: 0, SocketPath: socketPath}
	client1 := &Client{ID: 1, SocketPath: socketPath}

	require.NoError(t, connect(client0))
	require.Error(t, connect(client1), "expected an error when connecting too many clients")
}

func TestWaitStatusNonZero(t *testing.T) {
	runner := newRunner(t, 2)

	client0 := &Client{ID: 0, SocketPath: runner.conf.SocketPath}
	client1 := &Client{ID: 1, SocketPath: runner.conf.SocketPath}

	require.NoError(t, connect(client0))
	require.NoError(t, connect(client1))
	require.NoError(t, client0.Exit(1))
	require.NoError(t, client1.Exit(0))
	require.Equal(t, runner.WaitStatus().ExitStatus(), 1)
}

func TestInterrupt(t *testing.T) {
	runner := newRunner(t, 2)
	ctx := context.Background()
	client0 := &Client{ID: 0, SocketPath: runner.conf.SocketPath}

	require.NoError(t, connect(client0))
	require.NoError(t, runner.Interrupt())

	require.ErrorIs(t, client0.Await(ctx, RunStateWait), ErrInterrupt)
	require.Error(t, client0.Await(ctx, RunStateStart), ErrInterrupt)
	require.NoError(t, client0.Await(ctx, RunStateInterrupt))
}

func TestTerminate(t *testing.T) {
	runner := newRunner(t, 2)
	ctx := context.Background()
	client0 := &Client{ID: 0, SocketPath: runner.conf.SocketPath}

	require.NoError(t, connect(client0))
	require.NoError(t, runner.Terminate())

	require.ErrorContains(t, client0.Await(ctx, RunStateWait), rpc.ErrShutdown.Error())
	require.ErrorContains(t, client0.Await(ctx, RunStateStart), rpc.ErrShutdown.Error())
	require.ErrorContains(t, client0.Await(ctx, RunStateInterrupt), rpc.ErrShutdown.Error())
}

func newRunner(t *testing.T, clientCount int) *Runner {
	tempDir, err := os.MkdirTemp("", t.Name())
	require.NoError(t, err)
	socketPath := filepath.Join(tempDir, "bk.sock")
	t.Cleanup(func() {
		os.RemoveAll(tempDir)
	})
	runner := NewRunner(logger.Discard, RunnerConfig{
		SocketPath:         socketPath,
		ClientCount:        clientCount,
		ClientStartTimeout: 10 * time.Minute,
		ClientLostTimeout:  2 * time.Second,
	})
	runnerCtx, cancelRunner := context.WithCancel(context.Background())
	go runner.Run(runnerCtx)
	t.Cleanup(func() {
		cancelRunner()
	})

	// wait for runner to listen
	require.Eventually(t, func() bool {
		_, err := os.Lstat(socketPath)
		return err == nil

	}, time.Second*10, time.Millisecond, "expected socket file to exist")

	return runner
}

var (
	waitStatusSuccess  = waitStatus{Code: 0}
	waitStatusFailure  = waitStatus{Code: 1}
	waitStatusSignaled = waitStatus{Code: 0, SignalCode: intptr(1)}
)

func init() {
	gob.Register(new(waitStatus))
}

func intptr(x int) *int {
	return &x
}

// helper for ignoring the response from regular client.Connect
func connect(c *Client) error {
	_, err := c.Connect(context.Background())
	return err
}
