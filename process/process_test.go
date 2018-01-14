package process_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/buildkite/agent/process"
)

func TestProcessRuns(t *testing.T) {
	var processStarted bool

	p := process.Process{
		Script: os.Args[0],
		Env:    []string{"TEST_MAIN=tester"},
		StartCallback: func() {
			processStarted = true
		},
		LineCallback:       func(s string) {},
		LinePreProcessor:   func(s string) string { return s },
		LineCallbackFilter: func(s string) bool { return true },
	}

	if err := p.Start(); err != nil {
		t.Fatal(err)
	}

	if !processStarted {
		t.Fatal("StartCallback wasn't called")
	}
}

// Invoked by `go test`, switch between helper and running tests based on env
func TestMain(m *testing.M) {
	switch os.Getenv("TEST_MAIN") {
	case "tester":
		fmt.Println("Llamas are the best")
		os.Exit(0)
	default:
		os.Exit(m.Run())
	}
}
