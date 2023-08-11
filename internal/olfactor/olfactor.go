package olfactor

import (
	"io"

	"github.com/buildkite/agent/v3/internal/replacer"
)

// Olfactor may be used for 'sniffing' the output of a command.
type Olfactor struct {
	smell string
	smelt bool
}

// New returns a writer and an olfactor. Writes to the writer will be forwarded
// to `dst` and the olfactor will sniff for the given smell.
func New(dst io.Writer, smell string) (io.Writer, *Olfactor) {
	d := &Olfactor{
		smelt: false,
		smell: smell,
	}
	return replacer.New(dst, []string{d.smell}, func(b []byte) []byte {
		d.smelt = true
		return b
	}), d
}

// Smelt returns true if the olfactor smelt the smell.
func (d *Olfactor) Smelt() bool {
	return d.smelt
}
