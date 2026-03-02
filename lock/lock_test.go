package lock

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/buildkite/agent/v3/internal/agentapi"
	"github.com/buildkite/agent/v3/logger"
)

var testSocketCounter uint32

func testSocketPath() string {
	id := atomic.AddUint32(&testSocketCounter, 1)
	return filepath.Join(os.TempDir(), fmt.Sprintf("lock_test-%d-%d", os.Getpid(), id))
}

func testLogger(t *testing.T) logger.Logger {
	t.Helper()
	logger := logger.NewConsoleLogger(
		logger.NewTextPrinter(os.Stderr),
		func(c int) { t.Errorf("exit(%d)", c) },
	)
	return logger
}

func testServerAndClient(t *testing.T, ctx context.Context) (*agentapi.Server, *Client) {
	t.Helper()
	sockPath, logger := testSocketPath(), testLogger(t)
	svr, err := agentapi.NewServer(sockPath, logger)
	if err != nil {
		t.Fatalf("NewServer(%q, logger) = error %v", sockPath, err)
	}
	if err := svr.Start(); err != nil {
		t.Fatalf("svr.Start() = %v", err)
	}

	cli, err := agentapi.NewClient(ctx, sockPath)
	if err != nil {
		t.Fatalf("NewClient(ctx, %q) = error %v", sockPath, err)
	}

	// lock.NewClient takes the socket *directory*. Rather than temporarily
	// symlink the socket created above, I've manually created a client.
	return svr, &Client{client: cli}
}

func TestLockUnlock(t *testing.T) {
	t.Parallel()
	ctx, canc := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(canc)

	svr, cli := testServerAndClient(t, ctx)
	t.Cleanup(func() { svr.Close() }) //nolint:errcheck // best-effort cleanup in test

	// Lock it
	token, err := cli.Lock(ctx, "llama")
	if err != nil {
		t.Errorf("Client.Lock(ctx, llama) error = %v", err)
	}
	if token == "" {
		t.Errorf("Client.Lock(ctx, llama) = %q, want non-empty token", token)
	}

	// Try unlocking with the wrong token
	if err := cli.Unlock(ctx, "llama", "wrong token"); err == nil {
		t.Errorf("Client.Unlock(ctx, llama, wrong token) = %v, want non-nil error", err)
	}

	// Unlock with the correct token
	if err := cli.Unlock(ctx, "llama", token); err != nil {
		t.Errorf("Client.Unlock(ctx, llama, %q) = %v, want nil", token, err)
	}

	// Unlocking it again, even with the right token, should fail
	if err := cli.Unlock(ctx, "llama", token); err == nil {
		t.Errorf("Client.Unlock(ctx, llama, %q) = %v, want non-nil error", token, err)
	}
}

func TestLocker(t *testing.T) {
	t.Parallel()
	ctx, canc := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(canc)

	svr, cli := testServerAndClient(t, ctx)
	t.Cleanup(func() { svr.Close() }) //nolint:errcheck // best-effort cleanup in test

	// This constitutes a test by virtue of Lock/Unlock panicking on any
	// internal error.
	l := cli.Locker("llama")

	var wg sync.WaitGroup
	var locks int
	for range 10 {
		wg.Add(1)
		go func() {
			l.Lock()
			locks++
			l.Unlock()
			wg.Done()
		}()
	}

	wg.Wait()

	if got, want := locks, 10; got != want {
		t.Errorf("locks = %d, want %d", got, want)
	}
}

func TestDoOnce(t *testing.T) {
	t.Parallel()
	ctx, canc := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(canc)

	svr, cli := testServerAndClient(t, ctx)
	t.Cleanup(func() { svr.Close() }) //nolint:errcheck // best-effort cleanup in test

	var wg sync.WaitGroup
	var calls atomic.Int32
	for range 10 {
		wg.Add(1)
		go func() {
			if err := cli.DoOnce(ctx, "once", func() {
				calls.Add(1)
			}); err != nil {
				t.Errorf("Client.DoOnce(ctx, once, inc) = %v", err)
			}
			wg.Done()
		}()
	}

	wg.Wait()
	if got, want := calls.Load(), int32(1); got != want {
		t.Errorf("calls.Load() = %d, want %d", got, want)
	}
}
