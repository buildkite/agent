package clicommand

import (
	"errors"
	"fmt"
	"os"
)

// ExitError is used to signal that the command should exit with the exit code
// in `code`. It also wraps an error, which can be used to provide more context.
type ExitError struct {
	code  int
	inner error
}

// NewExitError returns ExitError with the given code and wrapped error.
func NewExitError(code int, err error) *ExitError {
	return &ExitError{code: code, inner: err}
}

// Code returns the exit code.
func (e *ExitError) Code() int {
	return e.code
}

// Error prints the message of the wrapped error. It ignores the exit code.
func (e *ExitError) Error() string {
	return e.inner.Error()
}

// Unwrap returns the wrapped error.
func (e *ExitError) Unwrap() error {
	return e.inner
}

// Is will return true if the target is an ExitError with the same code.
func (e *ExitError) Is(target error) bool {
	terr, ok := target.(*ExitError)
	return ok && e.code == terr.code
}

// SilentExitError instructs ExitCode to not log anything and just exit
// with status `code`.
type SilentExitError struct {
	code int
}

// NewSilentExitError returns SilentExitError with the given code.
func NewSilentExitError(code int) *SilentExitError {
	return &SilentExitError{code: code}
}

// Error prints a message with the exit code. It should not be used as the
// the purpose of this error is to not print anything.
func (e *SilentExitError) Error() string {
	return fmt.Sprintf("silently exited status %d", e.code)
}

// Code returns the exit code.
func (e *SilentExitError) Code() int {
	return e.code
}

// Is will return true if the target is a SilentExitError with the same code.
func (e *SilentExitError) Is(target error) bool {
	terr, ok := target.(*SilentExitError)
	return ok && e.code == terr.code
}

// PrintMessageAndReturnExitCode prints the error message to stderr, preceded by
// "buildkite-agent: fatal: " and returns the exit code for the given error. If `err` is a
// SilentExitError or ExitError, it will return the code from that. Otherwise
// it will return 0 for nil errors and 1 for all other errors. Also, when `err`
// is a SilentExitError, it will not print anything to stderr.
func PrintMessageAndReturnExitCode(err error) int {
	if err == nil {
		return 0
	}

	if serr := new(SilentExitError); errors.As(err, &serr) {
		return serr.Code()
	}

	fmt.Fprintf(os.Stderr, "buildkite-agent: fatal: %s\n", err)

	if eerr := new(ExitError); errors.As(err, &eerr) {
		return eerr.Code()
	}

	return 1
}
