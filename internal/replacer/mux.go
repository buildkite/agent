package replacer

import (
	"errors"
)

// Mux contains multiple replacers
type Mux struct {
	underlying []*Replacer
}

// NewMux returns a new mux with the given replacers.
func NewMux(rs ...*Replacer) *Mux {
	m := &Mux{
		underlying: make([]*Replacer, 0, len(rs)),
	}
	m.underlying = append(m.underlying, rs...)
	return m
}

// Reset resets all replacers with new needles (secrets).
func (m *Mux) Reset(needles []string) {
	for _, r := range m.underlying {
		r.Reset(needles)
	}
}

// Add adds needles to all replacers.
func (m *Mux) Add(needles ...string) {
	for _, r := range m.underlying {
		r.Add(needles...)
	}
}

// Append adds a replacer to the Mux.
func (m *Mux) Append(r *Replacer) {
	m.underlying = append(m.underlying, r)
}

// Flush flushes all replacers.
func (m *Mux) Flush() error {
	errs := make([]error, 0, len(m.underlying))
	for _, r := range m.underlying {
		errs = append(errs, r.Flush())
	}
	return errors.Join(errs...)
}
