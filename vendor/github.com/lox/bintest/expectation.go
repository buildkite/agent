package bintest

import (
	"bytes"
	"encoding/json"
	"errors"
	"sync"

	"github.com/lox/bintest/proxy"
)

// Expectation is used for setting expectations
type Expectation struct {
	sync.RWMutex

	// Name of the binary that the expectation is against
	name string

	// The sequence the expectation occurred in
	sequence int

	// Holds the arguments of the method.
	arguments Arguments

	// The exit code to return
	exitCode int

	// The command to execute and return the results of
	passthroughPath string

	// The function to call when executed
	callFunc func(*proxy.Call)

	// Amount of times this call has been called
	totalCalls int

	// Times expected to be called
	minCalls, maxCalls int

	// Buffers to copy to stdout and stderr
	writeStdout, writeStderr *bytes.Buffer
}

func (e *Expectation) Times(expect int) *Expectation {
	return e.MinTimes(expect).MaxTimes(expect)
}

func (e *Expectation) MinTimes(expect int) *Expectation {
	e.Lock()
	defer e.Unlock()
	if expect == InfiniteTimes {
		expect = 0
	}
	e.minCalls = expect
	return e
}

func (e *Expectation) MaxTimes(expect int) *Expectation {
	e.Lock()
	defer e.Unlock()
	e.maxCalls = expect
	return e
}

func (e *Expectation) Optionally() *Expectation {
	e.Lock()
	defer e.Unlock()
	e.minCalls = 0
	return e
}

func (e *Expectation) Once() *Expectation {
	return e.Times(1)
}

func (e *Expectation) NotCalled() *Expectation {
	return e.Times(0)
}

func (e *Expectation) AndExitWith(code int) *Expectation {
	e.Lock()
	defer e.Unlock()
	e.exitCode = code
	e.passthroughPath = ""
	return e
}

func (e *Expectation) AndWriteToStdout(s string) *Expectation {
	e.Lock()
	defer e.Unlock()
	e.writeStdout.WriteString(s)
	e.passthroughPath = ""
	return e
}

func (e *Expectation) AndWriteToStderr(s string) *Expectation {
	e.Lock()
	defer e.Unlock()
	e.writeStderr.WriteString(s)
	e.passthroughPath = ""
	return e
}

func (e *Expectation) AndPassthroughToLocalCommand(path string) *Expectation {
	e.Lock()
	defer e.Unlock()
	e.passthroughPath = path
	return e
}

func (e *Expectation) AndCallFunc(f func(*proxy.Call)) *Expectation {
	e.Lock()
	defer e.Unlock()
	e.callFunc = f
	e.passthroughPath = ""
	return e
}

func (e *Expectation) Check(t TestingT) bool {
	if e.minCalls != InfiniteTimes && e.totalCalls < e.minCalls {
		t.Logf("Expected [%s %s] to be called at least %d times, got %d",
			e.name, e.arguments.String(), e.minCalls, e.totalCalls,
		)
		return false
	} else if e.maxCalls != InfiniteTimes && e.totalCalls > e.maxCalls {
		t.Logf("Expected [%s %s] to be called at most %d times, got %d",
			e.name, e.arguments.String(), e.maxCalls, e.totalCalls,
		)
		return false
	}
	return true
}

func (e *Expectation) String() string {
	var stringer = struct {
		Name            string    `json:"name,omitempty"`
		Sequence        int       `json:"sequence,omitempty"`
		Arguments       Arguments `json:"args,omitempty"`
		ExitCode        int       `json:"exitCode,omitempty"`
		PassthroughPath string    `json:"passthrough,omitempty"`
		TotalCalls      int       `json:"calls,omitempty"`
		MinCalls        int       `json:"minCalls,omitempty"`
		MaxCalls        int       `json:"maxCalls,omitempty"`
	}{
		e.name, e.sequence, e.arguments, e.exitCode, e.passthroughPath, e.totalCalls, e.minCalls, e.maxCalls,
	}
	var out = bytes.Buffer{}
	_ = json.NewEncoder(&out).Encode(stringer)
	return out.String()
}

// expectationSet is a set of expectations
type ExpectationSet []*Expectation

// Match the best matching Expectation in a set, or returns an error if one isn't found
func (exp ExpectationSet) Match(args ...string) (*Expectation, error) {
	for _, e := range exp {
		e.RLock()
		defer e.RUnlock()

		if match, _ := e.arguments.Match(args...); match {
			if e.maxCalls == InfiniteTimes || e.totalCalls < e.maxCalls {
				return e, nil
			}
		}
	}

	return nil, errors.New("No expectations match")
}
