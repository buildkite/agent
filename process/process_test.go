package process_test

import (
	"fmt"
	"os"
	"os/signal"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/buildkite/agent/process"
)

func TestProcessRunsAndSignalsStartedAndStopped(t *testing.T) {
	var started int32
	var done int32

	p := process.Process{
		Script: []string{os.Args[0]},
		Env:    []string{"TEST_MAIN=tester"},
	}

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
	if err := p.Start(); err != nil {
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

func TestProcessCapturesOutputLineByLine(t *testing.T) {
	var lines []string
	var mu sync.Mutex

	p := process.Process{
		Script: []string{os.Args[0]},
		Env:    []string{"TEST_MAIN=tester"},
		Handler: func(line string) {
			mu.Lock()
			defer mu.Unlock()
			lines = append(lines, line)
		},
	}

	if err := p.Start(); err != nil {
		t.Error(err)
	}

	expected := []string{
		"+++ My header",
		"llamas",
		"and more llamas",
		"a very long line a very long line a very long line a very long line a very long line a very long line a very long line a very long line a very long line a very long line a very long line a very long line a very long line a very long line",
		"and some alpacas",
	}

	if !reflect.DeepEqual(expected, lines) {
		t.Fatalf("Unexpected lines: %v", lines)
	}
}

func TestProcessIsKilledGracefully(t *testing.T) {
	if runtime.GOOS == `windows` {
		t.Skip("Not supported on windows")
	}

	var lines []string
	var mu sync.Mutex

	p := process.Process{
		Script: []string{os.Args[0]},
		Env:    []string{"TEST_MAIN=tester-signal"},
		Handler: func(line string) {
			mu.Lock()
			defer mu.Unlock()
			lines = append(lines, line)
		},
	}

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		<-p.Started()

		// give the signal handler some time to install
		time.Sleep(time.Millisecond * 50)

		p.Kill(time.Millisecond * 20)
	}()

	if err := p.Start(); err != nil {
		t.Fatal(err)
	}

	wg.Wait()

	mu.Lock()
	defer mu.Unlock()

	output := strings.Join(lines, "\n")
	if output != `SIG terminated` {
		t.Fatalf("Bad output: %q", output)
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

	case "tester-signal":
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, os.Interrupt,
			syscall.SIGTERM,
			syscall.SIGINT,
		)
		sig := <-signals
		fmt.Printf("SIG %v", sig)
		os.Exit(0)

	default:
		os.Exit(m.Run())
	}
}
