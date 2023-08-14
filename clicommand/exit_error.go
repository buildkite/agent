package clicommand

import "errors"

// ExitError is used to signal to main.go that the command should exit with a
// the exit code in `code`.
type ExitError struct {
	code  int
	inner error
}

func NewExitError(code int, err error) *ExitError {
	return &ExitError{code: code, inner: err}
}

func (e *ExitError) Code() int {
	return e.code
}

func (e *ExitError) Error() string {
	return e.inner.Error()
}

func (e *ExitError) Unwrap() error {
	return e.inner
}

func (e *ExitError) Is(target error) bool {
	terr, ok := target.(*ExitError)
	return ok && e.code == terr.code && errors.Is(e.inner, terr.inner)
}
