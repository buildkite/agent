//go:build !windows

package process_test

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"github.com/buildkite/agent/v3/internal/process"
	"github.com/buildkite/agent/v3/logger"
)

func TestProcessNiceValue(t *testing.T) {
	t.Parallel()

	stdout := &bytes.Buffer{}

	p := process.New(logger.Discard, process.Config{
		Path:   os.Args[0],
		Env:    []string{"TEST_MAIN=tester-nice"},
		Stdout: stdout,
		Nice:   5,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	if err := p.Run(ctx); err != nil {
		t.Fatalf("p.Run(ctx) = %v", err)
	}

	// The child process reports its own nice value via syscall.Getpriority.
	if got, want := stdout.String(), "nice=5"; got != want {
		t.Errorf("child process reported %q, want %q", got, want)
	}
}
