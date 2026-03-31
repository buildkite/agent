package process_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/buildkite/agent/v4/internal/experiments"
	"github.com/buildkite/agent/v4/internal/process"
	"github.com/buildkite/agent/v4/logger"
)

func TestProcessOutput(t *testing.T) {
	t.Parallel()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	p := process.New(logger.Discard, process.Config{
		Path:   os.Args[0],
		Env:    []string{"TEST_MAIN=output"},
		Stdout: stdout,
		Stderr: stderr,
	})

	// wait for the process to finish
	if err := p.Run(t.Context()); err != nil {
		t.Fatalf("p.Run(ctx) = %v", err)
	}

	if got, want := stdout.String(), "llamas1\nllamas2\r\n"; got != want {
		t.Errorf("stdout.String() = %q, want %q", got, want)
	}

	if got, want := stderr.String(), "alpacas1\ralpacas2\n"; got != want {
		t.Errorf("stderr.String() = %q, want %q", got, want)
	}

	assertProcessDoesntExist(t, p)
}

func TestProcessOutputPTY(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("PTY not supported on windows")
	}

	stdout := &bytes.Buffer{}

	logger := logger.NewBuffer()
	p := process.New(logger, process.Config{
		Path:   os.Args[0],
		Env:    []string{"TEST_MAIN=output"},
		PTY:    true,
		Stdout: stdout,
	})

	// wait for the process to finish
	if err := p.Run(t.Context()); err != nil {
		t.Fatalf("p.Run() = %v", err)
	}

	// PTY by default maps LF (\n) to CR LF (\r\n); see experiments.PTYRaw
	if got, want := stdout.String(), "llamas1\r\nalpacas1\rllamas2\r\r\nalpacas2\r\n"; got != want {
		t.Errorf("stdout.String() = %q, want %q", got, want)
	}

	for _, line := range logger.Messages {
		t.Logf("Process.logger: %q\n", line)
	}

	assertProcessDoesntExist(t, p)
}

func TestProcessOutputPTY_PTYRawExperiment(t *testing.T) {
	ctx, _ := experiments.Enable(t.Context(), experiments.PTYRaw)

	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("PTY not supported on windows")
	}

	stdout := &bytes.Buffer{}

	logger := logger.NewBuffer()
	p := process.New(logger, process.Config{
		Path:   os.Args[0],
		Env:    []string{"TEST_MAIN=output"},
		PTY:    true,
		Stdout: stdout,
	})

	// wait for the process to finish
	if err := p.Run(ctx); err != nil {
		t.Fatalf("p.Run() = %v", err)
	}

	if got, want := stdout.String(), "llamas1\nalpacas1\rllamas2\r\nalpacas2\n"; got != want {
		t.Errorf("stdout.String() = %q, want %q", got, want)
	}

	for _, line := range logger.Messages {
		t.Logf("Process.logger: %q\n", line)
	}

	assertProcessDoesntExist(t, p)
}

func TestProcessOutputPTY_PTYRawExperimentWritesBeforeRawMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PTY not supported on windows")
	}

	ctx, _ := experiments.Enable(t.Context(), experiments.PTYRaw)

	originalHook := processAfterPTYStartHookSwap(func() {
		time.Sleep(50 * time.Millisecond)
	})
	t.Cleanup(func() {
		processAfterPTYStartHookSwap(originalHook)
	})

	stdout := &bytes.Buffer{}
	logger := logger.NewBuffer()
	p := process.New(logger, process.Config{
		Path:   os.Args[0],
		Env:    []string{"TEST_MAIN=output-slow-exit"},
		PTY:    true,
		Stdout: stdout,
	})

	if err := p.Run(ctx); err != nil {
		t.Fatalf("p.Run() = %v", err)
	}

	if got, want := stdout.String(), "llamas1\nalpacas1\rllamas2\r\nalpacas2\n"; got != want {
		t.Fatalf("stdout.String() = %q, want %q", got, want)
	}

	assertProcessDoesntExist(t, p)
}

func processAfterPTYStartHookSwap(next func()) func() {
	prev := process.AfterPTYStartHookGet()
	process.AfterPTYStartHookSet(next)
	return prev
}

func TestProcessInput(t *testing.T) {
	t.Parallel()

	stdout := &bytes.Buffer{}

	p := process.New(logger.Discard, process.Config{
		Path:   "tr",
		Args:   []string{"hw", "HW"},
		Stdin:  strings.NewReader("hello world"),
		Stdout: stdout,
	})
	// wait for the process to finish
	if err := p.Run(t.Context()); err != nil {
		t.Fatalf("p.Run() = %v", err)
	}
	if got, want := stdout.String(), "Hello World"; got != want {
		t.Errorf("stdout.String() = %q, want %q", got, want)
	}
	assertProcessDoesntExist(t, p)
}

func TestProcessRunsAndSignalsStartedAndStopped(t *testing.T) {
	t.Parallel()

	var started int32
	var done int32

	p := process.New(logger.Discard, process.Config{
		Path:              os.Args[0],
		Env:               []string{"TEST_MAIN=tester"},
		SignalGracePeriod: time.Millisecond,
	})

	var wg sync.WaitGroup
	wg.Go(func() {
		<-p.Started()
		atomic.AddInt32(&started, 1)
		<-p.Done()
		atomic.AddInt32(&done, 1)
	})

	// wait for the process to finish
	if err := p.Run(t.Context()); err != nil {
		t.Fatalf("p.Run() = %v", err)
	}

	// wait for our go routine to finish
	wg.Wait()

	if got, want := atomic.LoadInt32(&started), int32(1); got != want {
		t.Errorf("started = %d, want %d", got, want)
	}
	if got, want := atomic.LoadInt32(&done), int32(1); got != want {
		t.Errorf("done = %d, want %d", got, want)
	}

	assertProcessDoesntExist(t, p)
}

func TestProcessTerminatesWhenContextDone(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	stdoutr, stdoutw := io.Pipe()

	p := process.New(logger.Discard, process.Config{
		Path:              os.Args[0],
		Env:               []string{"TEST_MAIN=tester-no-handler"},
		Stdout:            stdoutw,
		SignalGracePeriod: time.Second,
	})

	go func() {
		defer func() { _ = stdoutw.Close() }()
		if err := p.Run(ctx); err != nil {
			t.Errorf("p.Run(ctx) = %v", err)
		}
	}()

	waitUntilReady(t, p, stdoutr)

	cancel()

	// wait until stdout is closed
	if _, err := io.ReadAll(stdoutr); err != nil {
		t.Errorf("error reading stdout: %s", err)
	}

	if runtime.GOOS != "windows" {
		if got, want := p.WaitStatus().Signaled(), true; got != want {
			t.Fatalf("p.WaitStatus().Signaled() = %t, want %t", got, want)
		}
	}

	<-p.Done()

	assertProcessDoesntExist(t, p)
}

func TestProcessWithSlowHandlerKilledWhenContextDone(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	stdoutr, stdoutw := io.Pipe()

	p := process.New(logger.Discard, process.Config{
		Path:              os.Args[0],
		Env:               []string{"TEST_MAIN=tester-slow-handler"},
		Stdout:            stdoutw,
		SignalGracePeriod: time.Millisecond,
	})

	go func() {
		defer func() { _ = stdoutw.Close() }()
		if err := p.Run(ctx); err != nil {
			t.Errorf("p.Run(ctx) = %v", err)
		}
	}()

	waitUntilReady(t, p, stdoutr)

	cancel()

	// wait until stdout is closed
	if _, err := io.ReadAll(stdoutr); err != nil {
		t.Errorf("error reading stdout: %s", err)
	}

	if runtime.GOOS != "windows" {
		if got, want := p.WaitStatus().Signaled(), true; got != want {
			t.Fatalf("p.WaitStatus().Signaled() = %t, want %t", got, want)
		}
	}

	<-p.Done()

	assertProcessDoesntExist(t, p)
}

func TestProcessInterrupts(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Works in windows, but not in docker")
	}

	ctx := t.Context()

	stdoutr, stdoutw := io.Pipe()

	p := process.New(logger.Discard, process.Config{
		Path:              os.Args[0],
		Env:               []string{"TEST_MAIN=tester-signal"},
		Stdout:            stdoutw,
		SignalGracePeriod: time.Millisecond,
	})

	go func() {
		defer func() { _ = stdoutw.Close() }()
		if err := p.Run(ctx); err != nil {
			t.Errorf("p.Run(ctx) = %v", err)
		}
	}()

	waitUntilReady(t, p, stdoutr)

	if err := p.Interrupt(); err != nil {
		t.Fatalf("p.Interrupt() = %v", err)
	}

	stdout, err := io.ReadAll(stdoutr)
	if err != nil {
		t.Fatalf("io.ReadAll(stdoutr) error = %v", err)
	}

	if got, want := string(stdout), "SIG terminated"; got != want {
		t.Errorf("io.ReadAll(stdoutr) = %q, want %q", got, want)
	}

	assertProcessDoesntExist(t, p)
}

func TestProcessInterruptsAfterDone(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Process groups not supported on windows")
		return
	}

	p := process.New(logger.Discard, process.Config{
		Path: os.Args[0],
		Env:  []string{"TEST_MAIN=tester-pgid"},
	})

	if err := p.Run(t.Context()); err != nil {
		t.Fatalf("p.Run() = %v", err)
	}

	<-p.Done()

	if err := p.Interrupt(); err != nil {
		t.Fatalf("p.Interrupt() = %v", err)
	}
}

func TestProcessInterruptsWithCustomSignal(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Works in windows, but not in docker")
	}

	ctx := t.Context()

	stdoutr, stdoutw := io.Pipe()

	p := process.New(logger.Discard, process.Config{
		Path:              os.Args[0],
		Env:               []string{"TEST_MAIN=tester-signal"},
		Stdout:            stdoutw,
		InterruptSignal:   process.SIGINT,
		SignalGracePeriod: time.Millisecond,
	})

	go func() {
		defer func() { _ = stdoutw.Close() }()
		if err := p.Run(ctx); err != nil {
			t.Errorf("p.Run(ctx) = %v", err)
		}
	}()

	waitUntilReady(t, p, stdoutr)

	if err := p.Interrupt(); err != nil {
		t.Fatalf("p.Interrupt() = %v", err)
	}

	stdout, err := io.ReadAll(stdoutr)
	if err != nil {
		t.Fatalf("io.ReadAll(stdoutr) error = %v", err)
	}

	if got, want := string(stdout), "SIG interrupt"; got != want {
		t.Errorf("io.ReadAll(stdoutr) = %q, want %q", got, want)
	}

	assertProcessDoesntExist(t, p)
}

func TestProcessSetsProcessGroupID(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Process groups not supported on windows")
		return
	}

	p := process.New(logger.Discard, process.Config{
		Path: os.Args[0],
		Env:  []string{"TEST_MAIN=tester-pgid"},
	})

	if err := p.Run(t.Context()); err != nil {
		t.Fatalf("p.Run() = %v", err)
	}

	assertProcessDoesntExist(t, p)
}

// TestProcessRunDoesNotHangWhenChildLeaksStdout is a regression test for a
// hook-hang: when Stdout/Stderr is an io.Writer that is not an *os.File (e.g.
// an *io.PipeWriter, as the agent uses to pipe hook output to its logger),
// os/exec allocates an internal OS pipe and a background copy goroutine.
// Cmd.Wait cannot return until that copy goroutine sees EOF, which requires
// every copy of the pipe write-end fd to be closed. A hook whose child
// backgrounds a grandchild that inherits stdout keeps the write-end open after
// the direct child exits, so without a bounded wait Cmd.Wait blocks forever
// (observed in CI as a 10-minute test timeout). The fix bounds the post-exit
// wait so Run returns promptly, and does not report the leaked pipe as a
// failure of an otherwise-clean process exit.
func TestProcessRunDoesNotHangWhenChildLeaksStdout(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("the stdout fd-inheritance pipe-leak mechanism is POSIX-specific")
	}

	// *io.PipeWriter is deliberately NOT an *os.File, forcing os/exec down the
	// internal-OS-pipe + copy-goroutine path that can hang.
	r, w := io.Pipe()

	// Drain the reader so the copy goroutine is never blocked on the write side.
	go io.Copy(io.Discard, r) //nolint:errcheck // best-effort drain

	p := process.New(logger.Discard, process.Config{
		Path:   os.Args[0],
		Env:    []string{"TEST_MAIN=leak-stdout"},
		Stdout: w,
		Stderr: w,
	})

	// Ensure the leaked grandchild is cleaned up regardless of outcome.
	t.Cleanup(func() {
		_ = p.Terminate()
		_ = w.Close()
	})

	done := make(chan error, 1)
	go func() { done <- p.Run(t.Context()) }()

	// The bound must exceed the fix's WaitDelay (so the fixed code has time to
	// bound the wait and return) but be far below the grandchild's lifetime (so
	// an unbounded wait fails deterministically).
	const bound = 30 * time.Second
	select {
	case err := <-done:
		// A clean process exit whose only wrinkle is the leaked pipe must not be
		// surfaced as a failure.
		if err != nil {
			t.Fatalf("p.Run() = %v, want nil (a leaked stdout pipe must not fail a clean exit)", err)
		}
	case <-time.After(bound):
		t.Fatalf("p.Run() did not return within %s: Cmd.Wait is hung on a leaked stdout pipe", bound)
	}
}

func assertProcessDoesntExist(t *testing.T, p *process.Process) {
	t.Helper()

	proc, err := os.FindProcess(p.Pid())
	if err != nil {
		return
	}
	if err := proc.Signal(syscall.Signal(0)); err == nil {
		t.Fatalf("Process %d exists and is running", p.Pid())
	}
}

func BenchmarkProcess(b *testing.B) {
	for b.Loop() {
		proc := process.New(logger.Discard, process.Config{
			Path: os.Args[0],
			Env:  []string{"TEST_MAIN=output"},
		})
		if err := proc.Run(b.Context()); err != nil {
			b.Fatalf("proc.Run() = %v", err)
		}
	}
}

// waitUntilReady waits for the process to start, then reads "Ready\n" from the
// pipe reader, and fails the test if it cannot or the string it reads is
// different.
func waitUntilReady(t *testing.T, p *process.Process, stdoutr *io.PipeReader) {
	t.Helper()
	<-p.Started()
	wantReady := "Ready\n"
	buf := make([]byte, len(wantReady))
	if _, err := io.ReadFull(stdoutr, buf); err != nil {
		t.Fatalf("io.ReadFull(stdoutr, buf) error = %v", err)
	}
	if got := string(buf); got != wantReady {
		t.Fatalf("io.ReadFull(stdoutr, buf) read %q, want %q", got, wantReady)
	}
}
