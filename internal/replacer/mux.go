package replacer

import (
	"errors"
)

// Mux contains multiple replacers
type Mux []*Replacer

// Flush flushes all replacers.
func (mux Mux) Flush() error {
	var errs []error
	for _, r := range mux {
		if err := r.Flush(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) != 0 {
		return errors.Join(errs...)
	}
	return nil
}

// Reset resets all replacers with new needles (secrets).
func (mux Mux) Reset(needles []string) {
	for _, r := range mux {
		r.Reset(needles)
	}
}
