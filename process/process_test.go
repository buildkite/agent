package process_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/agent/v3/process"
)

func TestProcessOutput(t *testing.T) {
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

	if got, want := stdout.String(), "llamas1llamas2"; got != want {
		t.Errorf("stdout.String() = %q, want %q", got, want)
	}

	if got, want := stderr.String(), "alpacas1alpacas2"; got != want {
		t.Errorf("stderr.String() = %q, want %q", got, want)
	}

	assertProcessDoesntExist(t, p)
}

func TestProcessOutputPTY(t *testing.T) {
	if runtime.GOOS == `windows` {
		t.Skip("PTY not supported on windows")
	}

	stdout := &bytes.Buffer{}

	p := process.New(logger.Discard, process.Config{
		Path:   os.Args[0],
		Env:    []string{"TEST_MAIN=output"},
		PTY:    true,
		Stdout: stdout,
	})

	// wait for the process to finish
	if err := p.Run(context.Background()); err != nil {
		t.Fatalf("p.Run() = %v", err)
	}

	if got, want := stdout.String(), "llamas1alpacas1llamas2alpacas2"; got != want {
		t.Errorf("stdout.String() = %q, want %q", got, want)
	}

	assertProcessDoesntExist(t, p)
}

func TestProcessInput(t *testing.T) {
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
	var started int32
	var done int32

	p := process.New(logger.Discard, process.Config{
		Path: os.Args[0],
		Env:  []string{"TEST_MAIN=tester"},
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

func TestProcessTerminatesWhenContextDoes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stdoutr, stdoutw := io.Pipe()

	p := process.New(logger.Discard, process.Config{
		Path:   os.Args[0],
		Env:    []string{"TEST_MAIN=tester-signal"},
		Stdout: stdoutw,
	})

	go func() {
		defer stdoutw.Close()
		if err := p.Run(ctx); err != nil {
			t.Errorf("p.Run(ctx) = %v", err)
		}
	}()

	waitUntilReady(t, stdoutr)

	cancel()

	// wait until stdout is closed
	io.ReadAll(stdoutr)

	if runtime.GOOS != "windows" {
		if got, want := p.WaitStatus().Signaled(), true; got != want {
			t.Fatalf("p.WaitStatus().Signaled() = %t, want %t", got, want)
		}
	}

	<-p.Done()

	assertProcessDoesntExist(t, p)
}

func TestProcessInterrupts(t *testing.T) {
	if runtime.GOOS == `windows` {
		t.Skip("Works in windows, but not in docker")
	}

	stdoutr, stdoutw := io.Pipe()

	p := process.New(logger.Discard, process.Config{
		Path:   os.Args[0],
		Env:    []string{"TEST_MAIN=tester-signal"},
		Stdout: stdoutw,
	})

	go func() {
		defer stdoutw.Close()
		if err := p.Run(context.Background()); err != nil {
			t.Errorf("p.Run(context.Background()) = %v", err)
		}
	}()

	waitUntilReady(t, stdoutr)

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

func TestProcessInterruptsWithCustomSignal(t *testing.T) {
	if runtime.GOOS == `windows` {
		t.Skip("Works in windows, but not in docker")
	}

	stdoutr, stdoutw := io.Pipe()

	p := process.New(logger.Discard, process.Config{
		Path:            os.Args[0],
		Env:             []string{"TEST_MAIN=tester-signal"},
		Stdout:          stdoutw,
		InterruptSignal: process.SIGINT,
	})

	go func() {
		defer stdoutw.Close()
		if err := p.Run(context.Background()); err != nil {
			t.Errorf("p.Run(context.Background()) = %v", err)
		}
	}()

	waitUntilReady(t, stdoutr)

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
	if runtime.GOOS == `windows` {
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
	for n := 0; n < b.N; n++ {
		proc := process.New(logger.Discard, process.Config{
			Path: os.Args[0],
			Env:  []string{"TEST_MAIN=output"},
		})
		if err := proc.Run(context.Background()); err != nil {
			b.Fatalf("proc.Run() = %v", err)
		}
	}
}

// waitUntilReady reads "Ready\n" from the pipe reader, and fails the test if
// it cannot or the string it reads is different.
func waitUntilReady(t *testing.T, stdoutr *io.PipeReader) {
	t.Helper()
	wantReady := "Ready\n"
	buf := make([]byte, len(wantReady))
	if _, err := io.ReadFull(stdoutr, buf); err != nil {
		t.Fatalf("io.ReadFull(stdoutr, buf) error = %v", err)
	}
	if got := string(buf); got != wantReady {
		t.Fatalf("io.ReadFull(stdoutr, buf) read %q, want %q", got, wantReady)
	}
}

// Invoked by `go test`, switch between helper and running tests based on env
func TestMain(m *testing.M) {
	switch os.Getenv("TEST_MAIN") {
	case "tester":
		for _, line := range strings.Split(strings.TrimSuffix(longTestOutput, "\n"), "\n") {
			fmt.Printf("%s\n", line)
			time.Sleep(time.Millisecond * 20)
		}
		os.Exit(0)

	case "output":
		fmt.Fprintf(os.Stdout, "llamas1")
		fmt.Fprintf(os.Stderr, "alpacas1")
		fmt.Fprintf(os.Stdout, "llamas2")
		fmt.Fprintf(os.Stderr, "alpacas2")
		os.Exit(0)

	case "tester-signal":
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, os.Interrupt,
			syscall.SIGTERM,
			syscall.SIGINT,
		)
		fmt.Println("Ready")
		fmt.Printf("SIG %v", <-signals)
		os.Exit(0)

	case "tester-pgid":
		pid := syscall.Getpid()
		pgid, err := process.GetPgid(pid)
		if err != nil {
			log.Fatal(err)
		}
		if pgid != pid {
			log.Fatalf("Bad pgid, expected %d, got %d", pid, pgid)
		}
		fmt.Printf("pid %d == pgid %d", pid, pgid)
		os.Exit(0)

	default:
		os.Exit(m.Run())
	}
}
