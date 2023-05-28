package plugin

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// DeprecatedNameErrors contains an aggregation of DeprecatedNameError
type DeprecatedNameErrors struct {
	errs []DeprecatedNameError
}

// Errors returns the contained set of errors in sorted order
func (e *DeprecatedNameErrors) Errors() []DeprecatedNameError {
	if e == nil {
		return []DeprecatedNameError{}
	}

	errs := e.errs
	sort.Slice(errs, func(i, j int) bool {
		if errs[i].old == errs[j].old {
			return errs[i].new < errs[j].new
		}
		return errs[i].old < errs[j].old
	})

	return errs
}

// Len is the length of the underlying slice or 0 if nil
func (e *DeprecatedNameErrors) Len() int {
	if e == nil {
		return 0
	}
	return len(e.errs)
}

// Error returns each contained error on a new line
func (e *DeprecatedNameErrors) Error() string {
	builder := strings.Builder{}
	for i, err := range e.Errors() {
		_, _ = builder.WriteString(err.Error())
		if i < len(e.errs)-1 {
			_, _ = builder.WriteRune('\n')
		}
	}
	return builder.String()
}

// Append adds DeprecatedNameError contained set and returns the reciver.
// Returning the reveiver is necessary to support appending to nil. So this
// should be used just like the builtin `append` function.
func (e *DeprecatedNameErrors) Append(errs ...DeprecatedNameError) *DeprecatedNameErrors {
	if e == nil {
		return &DeprecatedNameErrors{errs: errs}
	}

	e.errs = append(e.errs, errs...)

	return e
}

// Is returns true if and only if a error that is wrapped in target
// contains the same set of DeprecatedNameError as the receiver.
func (e *DeprecatedNameErrors) Is(target error) bool {
	if e == nil {
		return target == nil
	}

	var targetErr *DeprecatedNameErrors
	if !errors.As(target, &targetErr) {
		return false
	}

	dict := make(map[DeprecatedNameError]int, len(e.errs))
	for _, err := range e.errs {
		if c, exists := dict[err]; !exists {
			dict[err] = 1
		} else {
			dict[err] = c + 1
		}
	}

	for _, err := range targetErr.errs {
		c, exists := dict[err]
		if !exists {
			return false
		}
		dict[err] = c - 1
	}

	for _, v := range dict {
		if v != 0 {
			return false
		}
	}

	return true
}

// DeprecatedNameError contains information about environment variable names that
// are deprecated. Both the deprecated name and its replacement are held
type DeprecatedNameError struct {
	old string
	new string
}

func NewDeprecatedNameError(oldName, newName string) DeprecatedNameError {
	return DeprecatedNameError{old: oldName, new: newName}
}

func (e *DeprecatedNameError) Error() string {
	return fmt.Sprintf(" deprecated: %q\nreplacement: %q\n", e.old, e.new)
}

func (e *DeprecatedNameError) Is(target error) bool {
	if e == nil {
		return target == nil
	}

	var targetErr *DeprecatedNameError
	if !errors.As(target, &targetErr) {
		return false
	}

	return e.old == targetErr.old && e.new == targetErr.new
}
