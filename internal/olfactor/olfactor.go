package olfactor

import (
	"io"

	"github.com/buildkite/agent/v3/internal/replacer"
)

type Olfactor struct {
	smell string
	smelt bool
}

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

func (d *Olfactor) Smelt() bool {
	return d.smelt
}
