package process_test

import (
	"bytes"
	"fmt"
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

	"github.com/buildkite/agent/logger"
	"github.com/buildkite/agent/process"
)

func TestProcessOutput(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	p := process.NewProcess(logger.Discard, process.Config{
		Path:   os.Args[0],
		Env:    []string{"TEST_MAIN=output"},
		Stdout: stdout,
		Stderr: stderr,
	})

	// wait for the process to finish
	if err := p.Run(); err != nil {
		t.Fatal(err)
	}

	if s := stdout.String(); s != `llamas1llamas2` {
		t.Fatalf("Bad stdout, %q", s)
	}

	if s := stderr.String(); s != `alpacas1alpacas2` {
		t.Fatalf("Bad stderr, %q", s)
	}
}

func TestProcessRunsAndSignalsStartedAndStopped(t *testing.T) {
	var started int32
	var done int32

	p := process.NewProcess(logger.Discard, process.Config{
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
	if err := p.Run(); err != nil {
		t.Fatal(err)
	}

	// wait for our go routine to finish
	wg.Wait()

	if startedVal := atomic.LoadInt32(&started); startedVal != 1 {
		t.Fatalf("Expected started to be 1, got %d", startedVal)
	}

	if doneVal := atomic.LoadInt32(&done); doneVal != 1 {
		t.Fatalf("Expected done to be 1, got %d", doneVal)
	}

	if exitStatus := p.ExitStatus; exitStatus != "0" {
		t.Fatalf("Expected ExitStatus of 0, got %v", exitStatus)
	}
}

func TestProcessInterrupts(t *testing.T) {
	if runtime.GOOS == `windows` {
		t.Skip("Works in windows, but not in docker")
	}

	b := &bytes.Buffer{}

	p := process.NewProcess(logger.Discard, process.Config{
		Path:   os.Args[0],
		Env:    []string{"TEST_MAIN=tester-signal"},
		Stdout: b,
	})

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		<-p.Started()

		// give the signal handler some time to install
		time.Sleep(time.Millisecond * 50)

		p.Interrupt()
	}()

	if err := p.Run(); err != nil {
		t.Fatal(err)
	}

	wg.Wait()

	output := b.String()
	if output != `SIG terminated` {
		t.Fatalf("Bad output: %q", output)
	}
}

func TestProcessSetsProcessGroupID(t *testing.T) {
	if runtime.GOOS == `windows` {
		t.Skip("Process groups not supported on windows")
		return
	}

	p := process.NewProcess(logger.Discard, process.Config{
		Path: os.Args[0],
		Env:  []string{"TEST_MAIN=tester-pgid"},
	})

	if err := p.Run(); err != nil {
		t.Fatal(err)
	}

	if p.ExitStatus != "0" {
		t.Fatalf("Expected ExitStatus to be 0, got %s", p.ExitStatus)
	}
}

func BenchmarkProcess(b *testing.B) {
	for n := 0; n < b.N; n++ {
		proc := process.NewProcess(logger.Discard, process.Config{
			Path: os.Args[0],
			Env:  []string{"TEST_MAIN=output"},
		})
		if err := proc.Run(); err != nil {
			b.Fatal(err)
		}
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
