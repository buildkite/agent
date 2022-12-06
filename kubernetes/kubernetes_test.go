package kubernetes

import (
	"context"
	"encoding/gob"
	"os"
	"path/filepath"
	"syscall"
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
		require.NoError(t, err.Err)
		break
	case err := <-client1.WaitReady():
		require.NoError(t, err.Err)
		require.FailNow(t, "client1 should not be ready")
	case err := <-client2.WaitReady():
		require.NoError(t, err.Err)
		require.FailNow(t, "client2 should not be ready")
	case <-runner.Done():
		require.FailNow(t, "runner should not be done")
	case <-time.After(time.Second):
		require.FailNow(t, "client0 should be ready")
	}

	require.NoError(t, client0.Exit(waitStatusSuccess))
	select {
	case err := <-client1.WaitReady():
		require.NoError(t, err.Err)
		break
	case err := <-client2.WaitReady():
		require.NoError(t, err.Err)
		require.FailNow(t, "client2 should not be ready")
	case <-runner.Done():
		require.FailNow(t, "runner should not be done")
	case <-time.After(time.Second):
		require.FailNow(t, "client1 should be ready")
	}

	require.NoError(t, client1.Exit(waitStatusSuccess))
	select {
	case err := <-client2.WaitReady():
		require.NoError(t, err.Err)
		break
	case <-runner.Done():
		require.FailNow(t, "runner should not be done")
	case <-time.After(time.Second):
		require.FailNow(t, "client2 should be ready")
	}

	require.NoError(t, client2.Exit(waitStatusSuccess))
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

	require.NoError(t, client0.Connect())
	require.Error(t, client1.Connect(), "expected an error when connecting too many clients")
}

func TestWaitStatusNonZero(t *testing.T) {
	runner := newRunner(t, 2)

	client0 := Client{ID: 0, SocketPath: runner.conf.SocketPath}
	client1 := Client{ID: 1, SocketPath: runner.conf.SocketPath}

	require.NoError(t, client0.Connect())
	require.NoError(t, client1.Connect())
	require.NoError(t, client0.Exit(waitStatusFailure))
	require.NoError(t, client1.Exit(waitStatusSuccess))
	require.Equal(t, runner.WaitStatus().ExitStatus(), 1)
}

func TestWaitStatusSignaled(t *testing.T) {
	runner := newRunner(t, 2)

	client0 := Client{ID: 0, SocketPath: runner.conf.SocketPath}
	client1 := Client{ID: 1, SocketPath: runner.conf.SocketPath}

	require.NoError(t, client0.Connect())
	require.NoError(t, client1.Connect())
	require.NoError(t, client0.Exit(waitStatusSignaled))
	require.NoError(t, client1.Exit(waitStatusSuccess))
	require.Equal(t, runner.WaitStatus().ExitStatus(), 0)
	require.True(t, runner.WaitStatus().Signaled())
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

type waitStatus struct {
	Code       int
	SignalCode *int
}

func (w waitStatus) ExitStatus() int {
	return w.Code
}

func (w waitStatus) Signaled() bool {
	return w.SignalCode != nil
}

func (w waitStatus) Signal() syscall.Signal {
	return syscall.Signal(*w.SignalCode)
}

func intptr(x int) *int {
	return &x
}
