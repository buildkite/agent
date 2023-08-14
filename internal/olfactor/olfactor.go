package olfactor

import (
	"io"

	"github.com/buildkite/agent/v3/internal/replacer"
)

// Olfactor may be used for 'sniffing' an io stream for a string. In other
// words, the io stream can be monitored for a particular string, and if that
// string is written to the io stream, the olfactor will record that it has
// 'smelt' the string.
type Olfactor struct {
	smell string
	smelt bool
}

// New returns an io.Writer and an Olfactor. Writes to the writer will be
// forwarded to `dst` and the returned Olfactor will recored whether `smell`
// has been written to the io.Writer.
func New(dst io.Writer, smell string) (io.Writer, *Olfactor) {
	if smell == "" {
		return dst, &Olfactor{smell: "", smelt: true}
	}
	d := &Olfactor{smelt: false, smell: smell}
	return replacer.New(dst, []string{d.smell}, func(b []byte) []byte {
		d.smelt = true
		return b
	}), d
}

// Smelt returns true if and only if the olfactor smelt the smell.
func (d *Olfactor) Smelt() bool {
	return d.smelt
}
