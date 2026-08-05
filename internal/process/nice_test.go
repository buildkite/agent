//go:build !windows

package process_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"testing"
	"time"

	"github.com/buildkite/agent/v4/internal/process"
	"github.com/buildkite/agent/v4/logger"
)

func TestProcessNiceValue(t *testing.T) {
	t.Parallel()

	stdout := &bytes.Buffer{}
	stdinR, stdinW := io.Pipe()
	started := make(chan struct{})

	p := process.New(logger.Discard, process.Config{
		Path:    os.Args[0],
		Env:     []string{"TEST_MAIN=tester-nice"},
		Stdout:  stdout,
		Stdin:   stdinR,
		Nice:    5,
		Started: started,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	// Run the process in the background.
	errCh := make(chan error, 1)
	go func() { errCh <- p.Run(ctx) }()

	// Wait for the process to start (postStart has applied Setpriority).
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("timed out waiting for process to start")
	}

	// Signal the child that it's safe to read its priority.
	if _, err := stdinW.Write([]byte("g")); err != nil {
		t.Fatalf("writing start signal: %v", err)
	}
	if err := stdinW.Close(); err != nil {
		t.Fatalf("closing stdin pipe: %v", err)
	}

	if err := <-errCh; err != nil {
		t.Fatalf("p.Run(ctx) = %v", err)
	}

	// The child process reports its own nice value via syscall.Getpriority.
	if got, want := stdout.String(), "nice=5"; got != want {
		t.Errorf("child process reported %q, want %q", got, want)
	}
}
