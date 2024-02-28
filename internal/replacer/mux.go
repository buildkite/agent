package replacer

import (
	"errors"
)

type unit struct{}

// Mux contains multiple replacers
type Mux map[*Replacer]unit

// NewMux returns a new mux with the given replacers.
func NewMux(rs ...*Replacer) *Mux {
	m := Mux{}
	m.Append(rs...)
	return &m
}

// Reset resets all replacers with new needles (secrets).
func (mux Mux) Reset(needles []string) {
	for r := range mux {
		r.Reset(needles)
	}
}

// Add adds needles to all replacers in the mux.
func (mux Mux) Add(needles ...string) {
	for r := range mux {
		r.Add(needles...)
	}
}

// Flush flushes all replacers in the mux.
func (mux Mux) Flush() error {
	errs := make([]error, 0, len(mux))
	for r := range mux {
		errs = append(errs, r.Flush())
	}
	return errors.Join(errs...)
}

// Append appends the given replacers to the mux.
func (mux Mux) Append(rs ...*Replacer) {
	for _, r := range rs {
		mux[r] = unit{}
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
