package bintest

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/lox/bintest/proxy"
)

const (
	InfiniteTimes = -1
)

// TestingT is an interface for *testing.T
type TestingT interface {
	Logf(format string, args ...interface{})
	Errorf(format string, args ...interface{})
}

// Mock provides a wrapper around a Proxy for testing
type Mock struct {
	sync.Mutex

	// Name of the binary
	Name string

	// Path to the bintest binary
	Path string

	// Actual invocations that occurred
	invocations []Invocation

	// The executions expected of the binary
	expected ExpectationSet

	// A list of middleware functions to call before invocation
	before []func(i Invocation) error

	// Whether to ignore unexpected calls
	ignoreUnexpected bool

	// The related proxy
	proxy *proxy.Proxy

	// A command to passthrough execution to
	passthroughPath string
}

// Mock returns a new Mock instance, or fails if the bintest fails to compile
func NewMock(path string) (*Mock, error) {
	m := &Mock{}

	proxy, err := proxy.New(path)
	if err != nil {
		return nil, err
	}

	m.Name = filepath.Base(proxy.Path)
	m.Path = proxy.Path
	m.proxy = proxy

	go func() {
		for call := range m.proxy.Ch {
			m.invoke(call)
		}
	}()
	return m, nil
}

func (m *Mock) invoke(call *proxy.Call) {
	m.Lock()
	defer m.Unlock()

	debugf("Handling invocation for %s %s", m.Name, call.Args)

	var invocation = Invocation{
		Args: call.Args,
		Env:  call.Env,
		Dir:  call.Dir,
	}

	// Before we execute any invocations, run the before funcs
	for _, beforeFunc := range m.before {
		if err := beforeFunc(invocation); err != nil {
			fmt.Fprintf(call.Stderr, "\033[31m🚨 Error: %v\033[0m\n", err)
			call.Exit(1)
			return
		}
	}

	result := m.expected.ForArguments(call.Args...)
	expected, err := result.Match()
	if err != nil {
		debugf("No match found for expectation: %v", err)

		m.invocations = append(m.invocations, invocation)
		if m.ignoreUnexpected {
			debugf("Exiting silently, ignoreUnexpected is set")
			call.Exit(0)
		} else if err == ErrNoExpectationsMatch {
			fmt.Fprintf(call.Stderr, "\033[31m🚨 Error: %s\033[0m\n", result.ClosestMatch().Explain())
			call.Exit(1)
		} else {
			fmt.Fprintf(call.Stderr, "\033[31m🚨 Error: %v\033[0m\n", err)
			call.Exit(1)
		}
		return
	}

	debugf("Found expectation: %s", expected)

	invocation.Expectation = expected

	if m.passthroughPath != "" {
		call.Exit(m.invokePassthrough(m.passthroughPath, call))
	} else if expected.passthroughPath != "" {
		call.Exit(m.invokePassthrough(expected.passthroughPath, call))
	} else if expected.callFunc != nil {
		expected.callFunc(call)
	} else {
		_, _ = io.Copy(call.Stdout, expected.writeStdout)
		_, _ = io.Copy(call.Stderr, expected.writeStderr)
		call.Exit(expected.exitCode)
	}

	debugf("Incrementing total call of expected from %d to %d", expected.totalCalls, expected.totalCalls+1)
	expected.totalCalls++

	m.invocations = append(m.invocations, invocation)
}

func (m *Mock) invokePassthrough(path string, call *proxy.Call) int {
	debugf("Passing through to %s %v", path, call.Args)
	cmd := exec.Command(path, call.Args...)
	cmd.Env = call.Env
	cmd.Stdout = call.Stdout
	cmd.Stderr = call.Stderr
	cmd.Stdin = call.Stdin
	cmd.Dir = call.Dir

	var waitStatus syscall.WaitStatus
	if err := cmd.Run(); err != nil {
		debugf("Exited with error: %v", err)
		if exitError, ok := err.(*exec.ExitError); ok {
			waitStatus = exitError.Sys().(syscall.WaitStatus)
			return waitStatus.ExitStatus()
		} else {
			panic(err)
		}
	}

	debugf("Exited with 0")
	return 0
}

// PassthroughToLocalCommand executes the mock name as a local command (looked up in PATH) and then passes
// the result as the result of the mock. Useful for assertions that commands happen, but where
// you want the command to actually be executed.
func (m *Mock) PassthroughToLocalCommand() *Mock {
	m.Lock()
	defer m.Unlock()
	path, err := exec.LookPath(m.Name)
	if err != nil {
		panic(err)
	}
	m.passthroughPath = path
	return m
}

// IgnoreUnexpectedInvocations allows for invocations without matching call expectations
// to just silently return 0 and no output
func (m *Mock) IgnoreUnexpectedInvocations() *Mock {
	m.Lock()
	defer m.Unlock()
	m.ignoreUnexpected = true
	return m
}

// Before adds a middleware that is run before the Invocation is dispatched
func (m *Mock) Before(f func(i Invocation) error) *Mock {
	m.Lock()
	defer m.Unlock()
	if m.before == nil {
		m.before = []func(i Invocation) error{f}
	} else {
		m.before = append(m.before, f)
	}
	return m
}

// Expect creates an expectation that the mock will be called with the provided args
func (m *Mock) Expect(args ...interface{}) *Expectation {
	m.Lock()
	defer m.Unlock()
	ex := &Expectation{
		name:            m.Name,
		sequence:        len(m.expected) + 1,
		arguments:       Arguments(args),
		writeStderr:     &bytes.Buffer{},
		writeStdout:     &bytes.Buffer{},
		minCalls:        1,
		maxCalls:        1,
		passthroughPath: m.passthroughPath,
	}
	debugf("creating expectaion %s", ex)
	m.expected = append(m.expected, ex)
	return ex
}

// ExpectAll is a shortcut for adding lots of expectations
func (m *Mock) ExpectAll(argSlices [][]interface{}) {
	for _, args := range argSlices {
		m.Expect(args...)
	}
}

// Check that all assertions are met and that there aren't invocations that don't match expectations
func (m *Mock) Check(t TestingT) bool {
	m.Lock()
	defer m.Unlock()

	if len(m.expected) == 0 {
		return true
	}

	var failedExpectations, unexpectedInvocations int

	// first check that everything we expect
	for _, expected := range m.expected {
		if !expected.Check(t) {
			failedExpectations++
		}
	}

	if failedExpectations > 0 {
		t.Errorf("Not all expectations were met (%d out of %d)",
			len(m.expected)-failedExpectations,
			len(m.expected))
	}

	// next check if we have invocations without expectations
	if !m.ignoreUnexpected {
		for _, invocation := range m.invocations {
			if invocation.Expectation == nil {
				t.Logf("Unexpected call to %s %s",
					m.Name, FormatStrings(invocation.Args))
				unexpectedInvocations++
			}
		}

		if unexpectedInvocations > 0 {
			t.Errorf("More invocations than expected (%d vs %d)",
				unexpectedInvocations,
				len(m.invocations))
		}
	}

	return unexpectedInvocations == 0 && failedExpectations == 0
}

func (m *Mock) CheckAndClose(t TestingT) error {
	if err := m.proxy.Close(); err != nil {
		return err
	}
	if !m.Check(t) {
		return errors.New("Assertion checks failed")
	}
	return nil
}

func (m *Mock) Close() error {
	debugf("Closing mock")
	return m.proxy.Close()
}

// Invocation is a call to the binary
type Invocation struct {
	Args        []string
	Env         []string
	Dir         string
	Expectation *Expectation
}

var (
	Debug bool
)

func debugf(pattern string, args ...interface{}) {
	if Debug {
		log.Printf("[mock] "+pattern, args...)
	}
}
