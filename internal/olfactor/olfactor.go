package olfactor

import (
	"io"

	"github.com/buildkite/agent/v3/internal/replacer"
	"github.com/buildkite/agent/v3/internal/trie"
)

// Olfactor may be used for 'sniffing' an io stream for strings. In other
// words, the io stream can be monitored for a particular strings, and if they
// string are written to the io stream, the olfactor will record that it has
// 'smelt' that string.
type Olfactor struct {
	smelt *trie.Trie
}

// New returns an io.Writer and an Olfactor. Writes to the writer will be
// forwarded to `dst` and the returned Olfactor will record whether the
// elements of `smells` have been written to the io.Writer.
//
// If a smell is the empty string, we consider it to have been smelt, even if
// nothing was wrtten to the io.Writer. This is consistent with the notion that
// writing an empty string to a writer is the same as writing nothing to the
// writer.
func New(dst io.Writer, smells []string) (io.Writer, *Olfactor) {
	d := &Olfactor{smelt: trie.New()}
	return replacer.New(dst, smells, func(b []byte) []byte {
		d.smelt.Insert(string(b))
		return b
	}), d
}

// Smelt returns true if and only if the Olfactor smelt the smell.
func (d *Olfactor) Smelt(smell string) bool {
	return d != nil && d.smelt.PrefixExists(smell)
}

// AllSmelt returns all the smells that the Olfactor has smelt.
func (d *Olfactor) AllSmelt() []string {
	if d == nil {
		return []string{}
	}

	return d.smelt.Contents()
}
