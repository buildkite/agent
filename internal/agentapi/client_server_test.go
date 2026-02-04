package agentapi

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/buildkite/agent/v3/logger"
)

var testSocketCounter uint32

func testSocketPath() string {
	id := atomic.AddUint32(&testSocketCounter, 1)
	return filepath.Join(os.TempDir(), fmt.Sprintf("internal_agentapi_test-%d-%d", os.Getpid(), id))
}

func testLogger(t *testing.T) logger.Logger {
	t.Helper()
	logger := logger.NewConsoleLogger(
		logger.NewTextPrinter(os.Stderr),
		func(c int) { t.Errorf("exit(%d)", c) },
	)
	return logger
}

func testServerAndClient(t *testing.T, ctx context.Context) (*Server, *Client) {
	t.Helper()
	sockPath, logger := testSocketPath(), testLogger(t)
	svr, err := NewServer(sockPath, logger)
	if err != nil {
		t.Fatalf("NewServer(%q, logger) = error %v", sockPath, err)
	}
	if err := svr.Start(); err != nil {
		t.Fatalf("svr.Start() = %v", err)
	}

	cli, err := NewClient(ctx, sockPath)
	if err != nil {
		t.Fatalf("NewClient(ctx, %q) = error %v", sockPath, err)
	}

	return svr, cli
}

func TestPing(t *testing.T) {
	t.Parallel()
	ctx, canc := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(canc)

	svr, cli := testServerAndClient(t, ctx)
	t.Cleanup(func() { svr.Close() }) //nolint:errcheck // Server shutdown is best-effort

	if err := cli.Ping(ctx); err != nil {
		t.Errorf("cli.Ping(ctx) = %v", err)
	}
}

func TestLockOperations(t *testing.T) {
	t.Parallel()
	ctx, canc := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(canc)

	svr, cli := testServerAndClient(t, ctx)
	t.Cleanup(func() { svr.Close() }) //nolint:errcheck // Server shutdown is best-effort

	const key = "llama"

	// The lock should be empty before anything changes it.
	got, err := cli.LockGet(ctx, key)
	if err != nil {
		t.Errorf("cli.LockGet(ctx, %q) = error %v", key, err)
	}
	if want := ""; got != want {
		t.Errorf("cli.LockGet(ctx, %q) = %q, want %q", key, got, want)
	}

	// CAS should succeed at changing it from empty to another value.
	from, to := "", "Kuzco"
	got, ok, err := cli.LockCompareAndSwap(ctx, key, from, to)
	if err != nil {
		t.Errorf("cli.LockCompareAndSwap(ctx, %q, %q, %q) = error %v", key, from, to, err)
	}
	if got != to || !ok {
		t.Errorf("cli.LockCompareAndSwap(ctx, %q, %q, %q) = (%q, %t, nil); want (%q, %t, nil)", key, from, to, got, ok, to, true)
	}

	// CAS should deny a change that expects the original "from" value.
	// (Unless something has concurrently changed it back, but we're not testing
	// that.)
	to2 := "Yzma"
	got, ok, err = cli.LockCompareAndSwap(ctx, key, from, to2)
	if err != nil {
		t.Errorf("cli.LockCompareAndSwap(ctx, %q, %q, %q) = error %v", key, from, to2, err)
	}
	if got != to || ok {
		t.Errorf("cli.LockCompareAndSwap(ctx, %q, %q, %q) = (%q, %t, nil); want (%q, %t, nil)", key, from, to2, got, ok, to, false)
	}

	// Get should get the successfully-set value.
	got, err = cli.LockGet(ctx, key)
	if err != nil {
		t.Errorf("cli.LockGet(ctx, %q) = error %v", key, err)
	}
	if want := to; got != want {
		t.Errorf("cli.LockGet(ctx, %q) = %q, want %q", key, got, want)
	}
}
