package process_test

import (
	"fmt"
	"os"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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
		Script: os.Args[0],
		Env:    []string{"TEST_MAIN=tester"},
		StartCallback: func() {
			atomic.AddInt32(&started, 1)
		},
		LineCallback:       func(s string) {},
		LinePreProcessor:   func(s string) string { return s },
		LineCallbackFilter: func(s string) bool { return true },
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
		Script:        os.Args[0],
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

func TestProcessOutputIsSafeFromRaces(t *testing.T) {
	p := process.Process{
		Script:             os.Args[0],
		Env:                []string{"TEST_MAIN=tester"},
		LineCallback:       func(s string) {},
		LinePreProcessor:   func(s string) string { return s },
		LineCallbackFilter: func(s string) bool { return true },
	}

	// the job_runner has a for loop that calls IsRunning and Output, so this checks those are safe from races
	p.StartCallback = func() {
		for p.IsRunning() {
			t.Logf("Output: %s", p.Output())
			time.Sleep(time.Millisecond * 20)
		}
		t.Logf("Not running anymore")
	}

	if err := p.Start(); err != nil {
		t.Fatal(err)
	}

	output := p.Output()
	if output != string(longTestOutput) {
		t.Fatalf("Output was unexpected:\nWanted: %q\nGot:    %q\n", longTestOutput, output)
	}
}

// Invoked by `go test`, switch between helper and running tests based on env
func TestMain(m *testing.M) {
	switch os.Getenv("TEST_MAIN") {
	case "tester":
		fmt.Printf(longTestOutput)
		time.Sleep(time.Millisecond * 50)
		os.Exit(0)
	default:
		os.Exit(m.Run())
	}
}
