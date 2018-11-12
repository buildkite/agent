package process_test

import (
	"fmt"
	"os"
	"os/signal"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/buildkite/agent/logger"
	"github.com/buildkite/agent/process"
)

const longTestOutput = `+++ My header
llamas
and more llamas
a very long line a very long line a very long line a very long line a very long line a very long line a very long line a very long line a very long line a very long line a very long line a very long line a very long line a very long line
and some alpacas
`

func TestProcessRunsAndCallsStartCallback(t *testing.T) {
	var started int32

	p := process.Process{
		Script: []string{os.Args[0]},
		Env:    []string{"TEST_MAIN=tester"},
		StartCallback: func() {
			atomic.AddInt32(&started, 1)
		},
		LineCallback:       func(s string) {},
		LinePreProcessor:   func(s string) string { return s },
		LineCallbackFilter: func(s string) bool { return false },
	}

	if err := p.Start(); err != nil {
		t.Fatal(err)
	}

	if startedVal := atomic.LoadInt32(&started); startedVal != 1 {
		t.Fatalf("Expected started to be 1, got %d", startedVal)
	}

	if exitStatus := p.ExitStatus; exitStatus != "0" {
		t.Fatalf("Expected ExitStatus of 0, got %v", exitStatus)
	}

	output := p.Output()
	if output != string(longTestOutput) {
		t.Fatalf("Output was unexpected:\nWanted: %q\nGot:    %q\n", longTestOutput, output)
	}
}

func TestProcessCallsLineCallbacksForEachOutputLine(t *testing.T) {
	var lineCounter int32
	var lines []string
	var linesLock sync.Mutex

	p := process.Process{
		Script:        []string{os.Args[0]},
		Env:           []string{"TEST_MAIN=tester"},
		StartCallback: func() {},
		LineCallback: func(s string) {
			linesLock.Lock()
			defer linesLock.Unlock()
			lines = append(lines, s)
		},
		LinePreProcessor: func(s string) string {
			lineNumber := atomic.AddInt32(&lineCounter, 1)
			return fmt.Sprintf("#%d: chars %d", lineNumber, len(s))
		},
		LineCallbackFilter: func(s string) bool {
			return true
		},
	}

	if err := p.Start(); err != nil {
		t.Fatal(err)
	}

	linesLock.Lock()

	var expected = []string{
		`#1: chars 13`,
		`#2: chars 6`,
		`#3: chars 15`,
		`#4: chars 237`,
		`#5: chars 16`,
	}

	if !reflect.DeepEqual(expected, lines) {
		t.Fatalf("Lines was unexpected:\nWanted: %v\nGot:    %v\n", expected, lines)
	}
}

func TestProcessPrependsLinesWithTimestamps(t *testing.T) {
	p := process.Process{
		Script:             []string{os.Args[0]},
		Env:                []string{"TEST_MAIN=tester"},
		StartCallback:      func() {},
		LineCallback:       func(s string) {},
		LinePreProcessor:   func(s string) string { return s },
		LineCallbackFilter: func(s string) bool { return strings.HasPrefix(s, "+++") },
		Timestamp:          true,
	}

	if err := p.Start(); err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(p.Output()), "\n")

	if lines[0] != `+++ My header` {
		t.Fatalf("Expected first line to be %q, got %q", `+++ My header`, lines[0])
	}

	tsRegex := regexp.MustCompile(`^\[.+?\]`)

	for _, line := range lines[1:] {
		if !tsRegex.MatchString(line) {
			t.Fatalf("Line doesn't start with a timestamp: %s", line)
		}
	}
}

func TestProcessOutputIsSafeFromRaces(t *testing.T) {
	var counter int32

	p := process.Process{
		Script:             []string{os.Args[0]},
		Env:                []string{"TEST_MAIN=tester"},
		LineCallback:       func(s string) {},
		LinePreProcessor:   func(s string) string { return s },
		LineCallbackFilter: func(s string) bool { return false },
	}

	// the job_runner has a for loop that calls IsRunning and Output, so this checks those are safe from races
	p.StartCallback = func() {
		for p.IsRunning() {
			_ = p.Output()
			atomic.AddInt32(&counter, 1)
			time.Sleep(time.Millisecond * 10)
		}
	}

	if err := p.Start(); err != nil {
		t.Fatal(err)
	}

	output := p.Output()
	if output != string(longTestOutput) {
		t.Fatalf("Output was unexpected:\nWanted: %q\nGot:    %q\n", longTestOutput, output)
	}

	if counterVal := atomic.LoadInt32(&counter); counterVal < 10 {
		t.Fatalf("Expected counter to be at least 10, got %d", counterVal)
	}
}

func TestKillingProcess(t *testing.T) {
	logger.SetLevel(logger.DEBUG)

	p := process.Process{
		Script: []string{os.Args[0]},
		Env:    []string{"TEST_MAIN=tester-signal"},
		LineCallback: func(s string) {
			t.Logf("Line: %s", s)
		},
		LinePreProcessor:   func(s string) string { return s },
		LineCallbackFilter: func(s string) bool { return false },
	}

	var wg sync.WaitGroup
	wg.Add(1)

	p.StartCallback = func() {
		go func() {
			<-time.After(time.Millisecond * 10)
			if err := p.Kill(); err != nil {
				t.Error(err)
			}
		}()
	}

	go func() {
		defer wg.Done()
		if err := p.Start(); err != nil {
			t.Error(err)
		}
	}()

	wg.Wait()

	output := p.Output()
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
