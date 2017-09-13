package bintest

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/lox/bintest/proxy"
)

const (
	infinite = -1
)

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

	t *testing.T
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

	expected := m.findExpectedCall(call.Args...)
	if expected == nil {
		m.invocations = append(m.invocations, invocation)
		fmt.Fprintf(call.Stderr, "\033[31m🚨 Unexpected call: %s %s\033[0m", m.Name, formatStrings(call.Args))
		call.Exit(1)
		return
	}

	expected.Lock()
	defer expected.Unlock()

	invocation.Expectation = expected

	if m.passthroughPath == "" {
		_, _ = io.Copy(call.Stdout, expected.writeStdout)
		_, _ = io.Copy(call.Stderr, expected.writeStderr)
		call.Exit(expected.exitCode)
	} else {
		call.Exit(m.invokePassthrough(call))
	}

	expected.totalCalls++
	m.invocations = append(m.invocations, invocation)
}

func (m *Mock) invokePassthrough(call *proxy.Call) int {
	cmd := exec.Command(m.passthroughPath, call.Args...)
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
		parent:      m,
		arguments:   Arguments(args),
		writeStderr: &bytes.Buffer{},
		writeStdout: &bytes.Buffer{},
	}
	m.expected = append(m.expected, ex)
	return ex
}

func (m *Mock) findExpectedCall(args ...string) *Expectation {
	for _, call := range m.expected {
		if call.expectedCalls == infinite || call.expectedCalls >= 0 {
			if match, _ := call.arguments.Match(args...); match {
				return call
			}
		}
	}
	return nil
}

// ExpectAll is a shortcut for adding lots of expectations
func (m *Mock) ExpectAll(argSlices [][]interface{}) {
	for _, args := range argSlices {
		m.Expect(args...)
	}
}

// Check that all assertions are met and that there aren't invocations that don't match expectations
func (m *Mock) Check(t *testing.T) bool {
	m.Lock()
	defer m.Unlock()

	if len(m.expected) == 0 {
		return true
	}

	var failedExpectations, unexpectedInvocations int

	// first check that everything we expect
	for _, expected := range m.expected {
		if !m.wasCalled(expected.arguments) {
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

func (m *Mock) CheckAndClose(t *testing.T) error {
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
	sync.Mutex

	parent *Mock

	// Holds the arguments of the method.
	arguments Arguments

	// The exit code to return
	exitCode int

	// The command to execute and return the results of
	proxyTo string

	// Amount of times this call has been called
	totalCalls int

	// Amount of times this is expected to be called
	expectedCalls int

	// Buffers to copy to stdout and stderr
	writeStdout, writeStderr *bytes.Buffer
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

func ArgumentsFromStrings(s []string) Arguments {
	args := make([]interface{}, len(s))

	for idx, v := range s {
		args[idx] = v
	}

	return args
}

// Invocation is a call to the binary
type Invocation struct {
	Args        []string
	Env         []string
	Expectation *Expectation
}

type Arguments []interface{}

func (a Arguments) Match(x ...string) (bool, string) {
	for i, expected := range a {
		var formatFail = func(formatter string, args ...interface{}) string {
			return fmt.Sprintf("Argument #%d doesn't match: %s",
				i, fmt.Sprintf(formatter, args...))
		}

		if len(x) <= i {
			return false, formatFail("Expected %q, but missing an argument", expected)
		}

		var actual = x[i]

		if matcher, ok := expected.(Matcher); ok {
			if match, message := matcher.Match(actual); !match {
				return false, formatFail(message)
			}
		} else if s, ok := expected.(string); ok && s != actual {
			return false, formatFail("Expected %q, got %q", expected, actual)
		}
	}
	if len(x) > len(a) {
		return false, fmt.Sprintf(
			"Argument #%d doesn't match: Unexpected extra argument", len(a))
	}

	return true, ""
}

func (a Arguments) String() string {
	return formatInterfaces(a)
}

type Matcher interface {
	fmt.Stringer
	Match(s string) (bool, string)
}

type MatcherFunc struct {
	f   func(s string) (bool, string)
	str string
}

func (mf MatcherFunc) Match(s string) (bool, string) {
	return mf.f(s)
}

func (mf MatcherFunc) String() string {
	return mf.str
}

func MatchAny() Matcher {
	return MatcherFunc{
		f:   func(s string) (bool, string) { return true, "" },
		str: "*",
	}
}

func formatStrings(a []string) string {
	var s = make([]string, len(a))
	for idx := range a {
		s[idx] = fmt.Sprintf("%q", a[idx])
	}
	return strings.Join(s, " ")
}

func formatInterfaces(a []interface{}) string {
	var s = make([]string, len(a))
	for idx := range a {
		switch t := a[idx].(type) {
		case string:
			s[idx] = fmt.Sprintf("%q", t)
		case fmt.Stringer:
			s[idx] = fmt.Sprintf("%s", t.String())
		default:
			s[idx] = fmt.Sprintf("%v", t)
		}
	}
	return strings.Join(s, " ")
}
