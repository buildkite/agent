package shell

import (
	"fmt"
	"strings"
)

// OlfactoryError is returned from the RunWithOlfactor when the command exits
// with an non-zero exit code and the olfactor detects the `Smell`.
// It wraps the original error and records what the smell was.
type OlfactoryError struct {
	smells map[string]struct{}
	inner  error
}

// NewOlfactoryError returns an error that wraps the given error and records what
// the smell was. It is expected that err is analogous to exec.ExitError
func NewOlfactoryError(smells map[string]struct{}, err error) *OlfactoryError {
	return &OlfactoryError{
		smells: smells,
		inner:  err,
	}
}

// Error returns a message about the wrapped error and what the detected smells
// were.
func (e *OlfactoryError) Error() string {
	smells := &strings.Builder{}
	_, _ = smells.WriteRune('[')
	for smell := range e.smells {
		_, _ = smells.WriteString(smell)
	}
	_, _ = smells.WriteRune(']')
	return fmt.Sprintf("error running command: %v, detected: %s", e.inner, smells)
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
	if !ok {
		return false
	}

	for smell := range e.smells {
		if _, ok := terr.smells[smell]; !ok {
			return false
		}
	}

	return true
}

func (e *OlfactoryError) Smelt(smell string) bool {
	_, ok := e.smells[smell]
	return ok
}
