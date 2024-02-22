package replacer

import (
	"errors"
)

// Mux contains multiple replacers
type Mux map[*Replacer]struct{}

// Reset resets all replacers with new needles (secrets).
func (mux Mux) Reset(needles []string) {
	for r := range mux {
		r.Reset(needles)
	}
}

// Remove removes the given replacers from the mux. They will be flushed.
func (mux Mux) Remove(rs ...*Replacer) error {
	errs := make([]error, 0, len(rs))
	for _, r := range rs {
		delete(mux, r)
		errs = append(errs, r.Flush())
	}
	return errors.Join(errs...)
}
