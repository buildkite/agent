package kubernetes

import (
	"context"
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

	// wait for runner to listen
	require.Eventually(t, func() bool {
		_, err := os.Lstat(socketPath)
		return err == nil

	}, time.Second*10, time.Millisecond, "expected socket file to exist")

	for _, client := range clients {
		client.SocketPath = socketPath
		require.NoError(t, client.Connect())
		t.Cleanup(client.Close)
	}
	select {
	case err := <-client0.WaitReady():
		require.NoError(t, err)
		break
	case err := <-client1.WaitReady():
		require.NoError(t, err)
		require.FailNow(t, "client1 should not be ready")
	case err := <-client2.WaitReady():
		require.NoError(t, err)
		require.FailNow(t, "client2 should not be ready")
	case <-runner.Done():
		require.FailNow(t, "runner should not be done")
	case <-time.After(time.Second):
		require.FailNow(t, "client0 should be ready")
	}

	require.NoError(t, client0.Exit(0))
	select {
	case err := <-client1.WaitReady():
		require.NoError(t, err)
		break
	case err := <-client2.WaitReady():
		require.NoError(t, err)
		require.FailNow(t, "client2 should not be ready")
	case <-runner.Done():
		require.FailNow(t, "runner should not be done")
	case <-time.After(time.Second):
		require.FailNow(t, "client1 should be ready")
	}

	require.NoError(t, client1.Exit(0))
	select {
	case err := <-client2.WaitReady():
		require.NoError(t, err)
		break
	case <-runner.Done():
		require.FailNow(t, "runner should not be done")
	case <-time.After(time.Second):
		require.FailNow(t, "client2 should be ready")
	}

	require.NoError(t, client2.Exit(0))
	select {
	case <-runner.Done():
		break
	case <-time.After(time.Second):
		require.FailNow(t, "runner should be done when all clients have exited")
	}
}

func TestDuplicateClients(t *testing.T) {
	runner := newRunner(t, 2)
	socketPath := runner.conf.SocketPath

	client0 := Client{ID: 0, SocketPath: socketPath}
	client1 := Client{ID: 0, SocketPath: socketPath}

	// wait for runner to listen
	require.Eventually(t, func() bool {
		_, err := os.Lstat(socketPath)
		return err == nil

	}, time.Second*10, time.Millisecond, "expected socket file to exist")

	require.NoError(t, client0.Connect())
	require.Error(t, client1.Connect(), "expected an error when connecting a client with a duplicate ID")
}

func TestExcessClients(t *testing.T) {
	runner := newRunner(t, 1)
	socketPath := runner.conf.SocketPath

	client0 := Client{ID: 0, SocketPath: socketPath}
	client1 := Client{ID: 1, SocketPath: socketPath}

	// wait for runner to listen
	require.Eventually(t, func() bool {
		_, err := os.Lstat(socketPath)
		return err == nil

	}, time.Second*10, time.Millisecond, "expected socket file to exist")

	require.NoError(t, client0.Connect())
	require.Error(t, client1.Connect(), "expected an error when connecting too many clients")
}

func newRunner(t *testing.T, clientCount int) *Runner {
	tempDir, err := os.MkdirTemp("", t.Name())
	require.NoError(t, err)
	socketPath := filepath.Join(tempDir, "bk.sock")
	t.Cleanup(func() {
		os.RemoveAll(tempDir)
	})
	runner := New(logger.Discard, Config{
		SocketPath:  socketPath,
		ClientCount: clientCount,
	})
	runnerCtx, cancelRunner := context.WithCancel(context.Background())
	go runner.Run(runnerCtx)
	t.Cleanup(func() {
		cancelRunner()
	})
	return runner
}
