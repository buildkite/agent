package bintest

import (
	"bytes"
	"errors"
	"fmt"
	"io"
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
	expected []*Expectation

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

	var invocation = Invocation{
		Args: call.Args,
		Env:  call.Env,
	}

	expected, err := m.findMatchingExpectation(call.Args...)
	if err != nil {
		m.invocations = append(m.invocations, invocation)
		fmt.Fprintf(call.Stderr, "\033[31m🚨 Error: %v\033[0m", err)
		call.Exit(1)
		return
	}

	expected.Lock()
	defer expected.Unlock()

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

	expected.totalCalls++
	m.invocations = append(m.invocations, invocation)
}

func (m *Mock) invokePassthrough(path string, call *proxy.Call) int {
	cmd := exec.Command(path, call.Args...)
	cmd.Env = call.Env
	cmd.Stdout = call.Stdout
	cmd.Stderr = call.Stderr
	cmd.Stdin = call.Stdin
	cmd.Dir = call.Dir

	var waitStatus syscall.WaitStatus
	if err := cmd.Run(); err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			waitStatus = exitError.Sys().(syscall.WaitStatus)
			return waitStatus.ExitStatus()
		} else {
			panic(err)
		}
	}

	return 0
}

func (m *Mock) PassthroughToLocalCommand() *Mock {
	path, err := exec.LookPath(m.Name)
	if err != nil {
		panic(err)
	}

	m.passthroughPath = path
	return m
}

// Expect creates an expectation that the mock will be called with the provided args
func (m *Mock) Expect(args ...interface{}) *Expectation {
	m.Lock()
	defer m.Unlock()
	ex := &Expectation{
		parent:        m,
		arguments:     Arguments(args),
		writeStderr:   &bytes.Buffer{},
		writeStdout:   &bytes.Buffer{},
		expectedCalls: 1,
	}
	m.expected = append(m.expected, ex)
	return ex
}

func (m *Mock) findMatchingExpectation(args ...string) (*Expectation, error) {
	var possibleMatches = []*Expectation{}

	// log.Printf("Trying to match call [%s %s]", m.Name, formatStrings(args))
	for _, expectation := range m.expected {
		expectation.RLock()
		defer expectation.RUnlock()
		// log.Printf("Comparing to [%s]", expectation.String())
		if match, _ := expectation.arguments.Match(args...); match {
			// log.Printf("Matched args")
			possibleMatches = append(possibleMatches, expectation)
		}
	}

	// log.Printf("Found %d possible matches for [%s %s]", len(possibleMatches), m.Name, formatStrings(args))

	for _, expectation := range possibleMatches {
		if expectation.expectedCalls == InfiniteTimes || expectation.totalCalls < expectation.expectedCalls {
			// log.Printf("Matched %v", expectation)
			return expectation, nil
		}
	}

	if len(possibleMatches) > 0 {
		return nil, fmt.Errorf("Call count didn't match possible expectations for [%s %s]", m.Name, formatStrings(args))
	}

	// log.Printf("No match found")
	return nil, fmt.Errorf("No matching expectation found for [%s %s]", m.Name, formatStrings(args))
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
		if expected.expectedCalls > 0 && !m.wasCalled(expected.arguments) {
			t.Logf("Expected %s %s to be called", m.Name,
				expected.arguments.String(),
			)
			failedExpectations++
		}
	}

	if failedExpectations > 0 {
		t.Errorf("Not all expectations were met (%d out of %d)",
			len(m.expected)-failedExpectations,
			len(m.expected))
	}

	// next check if we have invocations without expectations
	for _, invocation := range m.invocations {
		if invocation.Expectation == nil {
			t.Logf("Unexpected call to %s %s",
				m.Name, formatStrings(invocation.Args))
			unexpectedInvocations++
		}
	}

	if unexpectedInvocations > 0 {
		t.Errorf("More invocations than expected (%d vs %d)",
			unexpectedInvocations,
			len(m.invocations))
	}

	return unexpectedInvocations == 0 && failedExpectations == 0
}

func (m *Mock) wasCalled(args Arguments) bool {
	for _, invocation := range m.invocations {
		if match, _ := args.Match(invocation.Args...); match {
			return true
		}
	}
	return false
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
	return m.proxy.Close()
}

// Expectation is used for setting expectations
type Expectation struct {
	sync.RWMutex

	parent *Mock

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

	// Amount of times this is expected to be called
	expectedCalls int

	// Buffers to copy to stdout and stderr
	writeStdout, writeStderr *bytes.Buffer
}

func (e *Expectation) Times(expected int) *Expectation {
	e.Lock()
	defer e.Unlock()
	e.expectedCalls = expected
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
	return e
}

func (e *Expectation) AndWriteToStdout(s string) *Expectation {
	e.Lock()
	defer e.Unlock()
	e.writeStdout.WriteString(s)
	return e
}

func (e *Expectation) AndWriteToStderr(s string) *Expectation {
	e.Lock()
	defer e.Unlock()
	e.writeStderr.WriteString(s)
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
	return e
}

func (e *Expectation) String() string {
	return fmt.Sprintf("%s %s", e.parent.Name, e.arguments.String())
}

// Invocation is a call to the binary
type Invocation struct {
	Args        []string
	Env         []string
	Expectation *Expectation
}
