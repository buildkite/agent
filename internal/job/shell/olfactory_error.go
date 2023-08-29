package shell

import "fmt"

// OlfactoryError is returned from the RunWithOlfactor when the command exits
// with an non-zero exit code and the olfactor detects the `Smell`.
// It wraps the original error and records what the smell was.
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

// Is returns true if the target is an OlfactoryError and the smells match. If
// you want to check the inner errors match, you should unwrap them and run
// errors.Is on the unwrapped errors.
func (e *OlfactoryError) Is(target error) bool {
	terr, ok := target.(*OlfactoryError)
	return ok && e.Smell == terr.Smell
}
