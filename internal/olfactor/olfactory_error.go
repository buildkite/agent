package olfactor

import (
	"errors"
	"fmt"
)

// OlfactoryError is returned from the Olfactor when it detects a smell.
// it wraps the error returned from the command that was run.
type OlfactoryError struct {
	Smell string
	inner error
}

// NewOlfactoryError returns an error that wraps the given error and records what
// the smell was. It is expected that err is analogous to exec.ExitError
func NewOlfactoryError(smell string, err error) *OlfactoryError {
	return &OlfactoryError{
		Smell: smell,
		inner: err,
	}
}

// Error returns a message about the wrapped error and what the smell was.
// the message switches the analogy from smelling to detecting so that users
// don't get distracted
func (e *OlfactoryError) Error() string {
	return fmt.Sprintf("error running command: %v, detected: %s", e.inner, e.Smell)
}

// Unwrap returns the wrapped error
func (e *OlfactoryError) Unwrap() error {
	return e.inner
}

// Is returns true if the target is an OlfactoryError and the inner errors are equal
func (e *OlfactoryError) Is(target error) bool {
	terr, ok := target.(*OlfactoryError)
	// the detected slices were sorted on the way in, so we can compare them directly
	return ok && e.Smell == terr.Smell && errors.Is(e.inner, terr.inner)
}
