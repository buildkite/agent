package clicommand

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/buildkite/agent/v4/internal/agentapi"
	"github.com/buildkite/agent/v4/logger"
)

// countingListener counts the connections that it accepted and that are still
// open, and records the highest count.
type countingListener struct {
	net.Listener
	open, peak atomic.Int32
}

func (l *countingListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	// http.Server calls Accept from one goroutine, so only Accept writes peak.
	if n := l.open.Add(1); n > l.peak.Load() {
		l.peak.Store(n)
	}
	return &countedConn{Conn: c, l: l}, nil
}

type countedConn struct {
	net.Conn
	l    *countingListener
	once sync.Once
}

func (c *countedConn) Close() error {
	c.once.Do(func() { c.l.open.Add(-1) })
	return c.Conn.Close()
}

func TestLeaderPinger_ReusesConnection(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("leader symlinks are not supported on Windows")
	}

	// Unix socket paths have a short length limit, so t.TempDir can be too long.
	dir, err := os.MkdirTemp("", "lp")
	if err != nil {
		t.Fatalf("os.MkdirTemp() error = %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	leaderSock := filepath.Join(dir, "leader.sock")
	ln, err := net.Listen("unix", leaderSock)
	if err != nil {
		t.Fatalf("net.Listen(unix, %q) error = %v", leaderSock, err)
	}
	cl := &countingListener{Listener: ln}

	svr := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(agentapi.PingResponse{Now: time.Now()})
	})}
	go func() { _ = svr.Serve(cl) }()
	t.Cleanup(func() { _ = svr.Close() })

	leaderPath := filepath.Join(dir, "agent-leader")
	if err := os.Symlink(leaderSock, leaderPath); err != nil {
		t.Fatalf("os.Symlink() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	lb := logger.NewBuffer()
	t.Cleanup(func() { t.Log(lb.Messages) })
	leaderPinger(ctx, lb, filepath.Join(dir, "follower.sock"), leaderPath)

	// One connection for the socket test dial in NewClient, and at most two
	// idle connections for the pings (the http.Transport default for
	// MaxIdleConnsPerHost). Allow more for the extra connections that a slow
	// ping can open on a loaded host.
	if got, want := cl.peak.Load(), int32(8); got > want {
		t.Errorf("leader had up to %d open connections in 1s, want at most %d", got, want)
	}

	if d, err := os.Readlink(leaderPath); err != nil || d != leaderSock {
		t.Errorf("os.Readlink(%q) = %q, %v; want %q, nil (no coup)", leaderPath, d, err, leaderSock)
	}
}
