package bintest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/sasha-s/go-deadlock"
)

// Expectation is used for setting expectations
type Expectation struct {
	deadlock.RWMutex

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
	callFunc func(*Call)

	// A custom argument matcher function
	matcherFunc func(arg ...string) ArgumentsMatchResult

	// Amount of times this call has been called
	totalCalls int

	// Times expected to be called
	minCalls, maxCalls int

	// Buffers to copy to stdout and stderr
	writeStdout, writeStderr *bytes.Buffer
}

// Exactly expects exactly n invocations of this expectation
func (e *Expectation) Exactly(expect int) *Expectation {
	return e.Min(expect).Max(expect)
}

// Min expects a minimum of n invocations of this expectation
func (e *Expectation) Min(expect int) *Expectation {
	e.Lock()
	defer e.Unlock()
	if expect == InfiniteTimes {
		expect = 0
	}
	e.minCalls = expect
	return e
}

// Max expects a maximum of n invocations of this expectation, defaults to 1
func (e *Expectation) Max(expect int) *Expectation {
	e.Lock()
	defer e.Unlock()
	e.maxCalls = expect
	return e
}

// Optionally is a shortcut for Min(0)
func (e *Expectation) Optionally() *Expectation {
	e.Lock()
	defer e.Unlock()
	e.minCalls = 0
	return e
}

// NotCalled is a shortcut for Exactly(0)
func (e *Expectation) NotCalled() *Expectation {
	return e.Exactly(0)
}

// Once is a shortcut for Exactly(1)
func (e *Expectation) Once() *Expectation {
	return e.Exactly(1)
}

// AtLeastOnce expects a minimum invocations of 0 and a max of InfinityTimes
func (e *Expectation) AtLeastOnce() *Expectation {
	return e.Min(1).Max(InfiniteTimes)
}

// AndEndsWith causes the invoker to finish with an exit code of code
func (e *Expectation) AndExitWith(code int) *Expectation {
	e.Lock()
	defer e.Unlock()
	e.exitCode = code
	e.passthroughPath = ""
	return e
}

// AndWriteToStdout causes the invoker to output s to stdout. This resets any passthrough path set
func (e *Expectation) AndWriteToStdout(s string) *Expectation {
	e.Lock()
	defer e.Unlock()
	e.writeStdout.WriteString(s)
	e.passthroughPath = ""
	return e
}

// AndWriteToStdout causes the invoker to output s to stderr. This resets any passthrough path set
func (e *Expectation) AndWriteToStderr(s string) *Expectation {
	e.Lock()
	defer e.Unlock()
	e.writeStderr.WriteString(s)
	e.passthroughPath = ""
	return e
}

// AndPassthroughToLocalCommand causes the invoker to defer to a local command
func (e *Expectation) AndPassthroughToLocalCommand(path string) *Expectation {
	e.Lock()
	defer e.Unlock()
	e.passthroughPath = path
	return e
}

// AndCallFunc causes a middleware function to be called before invocation
func (e *Expectation) AndCallFunc(f func(*Call)) *Expectation {
	e.Lock()
	defer e.Unlock()
	e.callFunc = f
	e.passthroughPath = ""
	return e
}

// AnyArguments is a helper function for matching any argument set in WithMatcherFunc
func AnyArguments() func(arg ...string) ArgumentsMatchResult {
	return func(arg ...string) ArgumentsMatchResult {
		return ArgumentsMatchResult{
			IsMatch:    true,
			MatchCount: len(arg),
		}
	}
}

// WithMatcherFunc provides a custom matcher for argument sets, for instance matching variable amounts of
// arguments
func (e *Expectation) WithMatcherFunc(f func(arg ...string) ArgumentsMatchResult) *Expectation {
	e.Lock()
	defer e.Unlock()
	e.matcherFunc = f
	return e
}

// WithAnyArguments causes the expectation to accept any arguments via a MatcherFunc
func (e *Expectation) WithAnyArguments() *Expectation {
	return e.WithMatcherFunc(AnyArguments())
}

// Check evaluates the expectation and outputs failures to the provided testing.T object
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
	return strings.TrimSpace(out.String())
}

var ErrNoExpectationsMatch = errors.New("No expectations match")

// ExpectationResult is the result of a set of Arguments applied to an Expectation
type ExpectationResult struct {
	Arguments            []string
	Expectation          *Expectation
	ArgumentsMatchResult ArgumentsMatchResult
	CallCountMatch       bool
}

// ExpectationResultSet is a collection of ExpectationResult
type ExpectationResultSet []ExpectationResult

// ExactMatch returns the first Expectation that matches exactly
func (r ExpectationResultSet) Match() (*Expectation, error) {
	for _, row := range r {
		if row.ArgumentsMatchResult.IsMatch && row.CallCountMatch {
			return row.Expectation, nil
		}
	}
	return nil, ErrNoExpectationsMatch
}

// BestMatch returns the ExpectationResult that was the closest match (if not the exact)
// This is used for suggesting what the user might have meant
func (r ExpectationResultSet) ClosestMatch() ExpectationResult {
	var closest ExpectationResult
	var bestCount int
	var matched bool

	for _, row := range r {
		if row.ArgumentsMatchResult.MatchCount > bestCount {
			bestCount = row.ArgumentsMatchResult.MatchCount
			closest = row
			matched = true
		}
	}

	// if no arguments match, but there are expectations, return the first
	if !matched && len(r) > 0 {
		return r[0]
	}

	return closest
}

// Explain returns an explanation of why the Expectation didn't match
func (r ExpectationResult) Explain() string {
	if r.Expectation == nil {
		return "No expectations matched call"
	} else if r.ArgumentsMatchResult.IsMatch && !r.CallCountMatch {
		return fmt.Sprintf("Arguments matched, but total calls of %d would exceed maxCalls of %d",
			r.Expectation.totalCalls+1, r.Expectation.maxCalls)
	} else if !r.ArgumentsMatchResult.IsMatch {
		return r.ArgumentsMatchResult.Explanation
	}
	return "Expectation matched"
}

// ExpectationSet is a set of expectations
type ExpectationSet []*Expectation

// ForArguments applies arguments to the expectations and returns the results
func (exp ExpectationSet) ForArguments(args ...string) (result ExpectationResultSet) {
	for _, e := range exp {
		e.RLock()
		defer e.RUnlock()

		var argResult ArgumentsMatchResult

		// If provided, use a custom function for matching
		if e.matcherFunc != nil {
			argResult = e.matcherFunc(args...)
		} else {
			argResult = e.arguments.Match(args...)
		}

		result = append(result, ExpectationResult{
			Arguments:            args,
			Expectation:          e,
			ArgumentsMatchResult: argResult,
			CallCountMatch:       (e.maxCalls == InfiniteTimes || e.totalCalls < e.maxCalls),
		})
	}

	return
}
