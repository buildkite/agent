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

	"github.com/buildkite/agent/v3/internal/experiments"
	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/agent/v3/process"
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
	if err := p.Run(context.Background()); err != nil {
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
	if err := p.Run(context.Background()); err != nil {
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
	ctx, _ := experiments.Enable(context.Background(), experiments.PTYRaw)

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
	if err := p.Run(context.Background()); err != nil {
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
	wg.Add(1)

	go func() {
		defer wg.Done()

		<-p.Started()
		atomic.AddInt32(&started, 1)
		<-p.Done()
		atomic.AddInt32(&done, 1)
	}()

	// wait for the process to finish
	if err := p.Run(context.Background()); err != nil {
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stdoutr, stdoutw := io.Pipe()

	p := process.New(logger.Discard, process.Config{
		Path:              os.Args[0],
		Env:               []string{"TEST_MAIN=tester-no-handler"},
		Stdout:            stdoutw,
		SignalGracePeriod: time.Second,
	})

	go func() {
		defer stdoutw.Close()
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stdoutr, stdoutw := io.Pipe()

	p := process.New(logger.Discard, process.Config{
		Path:              os.Args[0],
		Env:               []string{"TEST_MAIN=tester-slow-handler"},
		Stdout:            stdoutw,
		SignalGracePeriod: time.Millisecond,
	})

	go func() {
		defer stdoutw.Close()
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

	ctx := context.Background()

	stdoutr, stdoutw := io.Pipe()

	p := process.New(logger.Discard, process.Config{
		Path:              os.Args[0],
		Env:               []string{"TEST_MAIN=tester-signal"},
		Stdout:            stdoutw,
		SignalGracePeriod: time.Millisecond,
	})

	go func() {
		defer stdoutw.Close()
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

	if err := p.Run(context.Background()); err != nil {
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

	ctx := context.Background()

	stdoutr, stdoutw := io.Pipe()

	p := process.New(logger.Discard, process.Config{
		Path:              os.Args[0],
		Env:               []string{"TEST_MAIN=tester-signal"},
		Stdout:            stdoutw,
		InterruptSignal:   process.SIGINT,
		SignalGracePeriod: time.Millisecond,
	})

	go func() {
		defer stdoutw.Close()
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

	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("p.Run() = %v", err)
	}

	assertProcessDoesntExist(t, p)
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
		if err := proc.Run(context.Background()); err != nil {
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
