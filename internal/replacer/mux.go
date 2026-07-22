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

// Needles returns the current set of needles (secrets). All replacers in a Mux
// are kept in sync by Add/Reset, so the needles of the first replacer are
// representative of the whole Mux. Returns nil if the Mux is empty.
func (m *Mux) Needles() []string {
	if len(m.underlying) == 0 {
		return nil
	}
	return m.underlying[0].Needles()
}

// Flush flushes all replacers.
func (m *Mux) Flush() error {
	errs := make([]error, 0, len(m.underlying))
	for _, r := range m.underlying {
		errs = append(errs, r.Flush())
	}
	return errors.Join(errs...)
}
