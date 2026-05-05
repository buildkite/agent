//go:build !windows

package kubernetes

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/buildkite/agent/v3/internal/job"
	"github.com/buildkite/agent/v3/logger"
)

// TestRepro3384_K8sRunnerLeaksBareContextError reproduces the user-visible bug
// in #3384: when the agent context is cancelled (UI cancel or job timeout),
// kubernetes.Runner.Run returns ctx.Err() bare. The pre-fix line at
// agent/run_job.go was:
//
//	fmt.Fprintf(r.jobLogs, "Error running job: %s\n", err)
//
// which produced `Error running job: context canceled` in the job log. This
// test drives a real Runner, cancels its context, and shows both the pre-fix
// and post-fix log lines.
func TestRepro3384_K8sRunnerLeaksBareContextError(t *testing.T) {
	// Short dir name: macOS unix sockets cap at 104 bytes.
	tempDir, err := os.MkdirTemp("", "r3384")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tempDir) })

	runner := NewRunner(logger.Discard, RunnerConfig{
		SocketPath:  filepath.Join(tempDir, "bk.sock"),
		ClientCount: 0, // startupCheck disabled with ClientStartTimeout==0
	})

	ctx, cancel := context.WithCancel(context.Background())
	runErrCh := make(chan error, 1)
	go func() { runErrCh <- runner.Run(ctx) }()

	// Wait for the socket to appear so we know Run is in its select loop.
	deadline := time.After(10 * time.Second)
	tick := time.Tick(time.Millisecond)
loop:
	for {
		select {
		case <-tick:
			if _, err := os.Lstat(runner.conf.SocketPath); err == nil {
				break loop
			}
		case <-deadline:
			t.Fatal("runner socket never appeared")
		}
	}

	// Simulate the agent receiving a cancel (UI cancel / job timeout).
	cancel()

	var runErr error
	select {
	case runErr = <-runErrCh:
	case <-time.After(5 * time.Second):
		t.Fatal("runner.Run did not return after ctx cancel")
	}

	// Confirm the bare context error.
	if !errors.Is(runErr, context.Canceled) {
		t.Fatalf("runner.Run returned %v, want context.Canceled", runErr)
	}
	if runErr.Error() != "context canceled" {
		t.Fatalf("runErr.Error() = %q, want %q (Go's bare sentinel)", runErr.Error(), "context canceled")
	}

	// Pre-fix: agent/run_job.go printed err directly.
	preFix := fmt.Sprintf("Error running job: %s", runErr)
	t.Logf("PRE-FIX  job log line: %q", preFix)
	if preFix != "Error running job: context canceled" {
		t.Fatalf("pre-fix line = %q, want %q", preFix, "Error running job: context canceled")
	}

	// Post-fix: routed through job.FormatJobError.
	postFix := fmt.Sprintf("Error running job: %s", job.FormatJobError(runErr))
	t.Logf("POST-FIX job log line: %q", postFix)
	if postFix != "Error running job: job cancelled" {
		t.Fatalf("post-fix line = %q, want %q", postFix, "Error running job: job cancelled")
	}
}
