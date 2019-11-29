package process

import (
	"bytes"
	"io"
	"regexp"
	"unicode/utf8"
)

// This regex matches "Erase in Line" escape characters:
// [K Clear from cursor to the end of the line
// [1K Clear from cursor to beginning of the line
// [2K Clear entire line
var lineBreakEscapeCharRegex = regexp.MustCompile(`^\[[012]?K`)

type Prefixer struct {
	w       io.Writer
	f       func() string
	initial bool
}

func NewPrefixer(w io.Writer, f func() string) *Prefixer {
	return &Prefixer{
		w:       w,
		f:       f,
		initial: true,
	}
}

func (p *Prefixer) Write(data []byte) (n int, err error) {
	// if not already written, write the initial prefix
	if p.initial {
		n, err := p.w.Write([]byte(p.f()))
		if err != nil {
			return n, err
		}
		p.initial = false
	}

	offset := 0
	out := make([]byte, 0, len(data))

	// loop through line breaks and add prefix
	for l := len(data); offset < l; {
		// find either a newline or an escape char
		next := bytes.IndexAny(data[offset:], "\n\x1b")
		if next == -1 {
			break
		}

		// decode the first rune in the string of the match
		r, _ := utf8.DecodeRune(data[offset+next:])
		switch r {
		case '\n':
			out = append(out, data[offset:offset+next+1]...)
			out = append(out, []byte(p.f())...)
			offset = offset + next + 1
		case '\x1b':
			match := lineBreakEscapeCharRegex.Find(data[offset+next+1:])
			if match != nil {
				out = append(out, data[offset:offset+next+len(match)+1]...)
				out = append(out, []byte(p.f())...)
				offset = offset + next + len(match) + 1
			} else {
				out = append(out, data[offset:offset+next+1]...)
				offset = offset + next + 1
			}
		default:
			out = append(out, data[offset:offset+next+1]...)
			offset = offset + next + 1
		}
	}

	// add any left overs
	if offset < len(data) {
		out = append(out, data[offset:]...)
	}

	if _, err := p.w.Write(out); err != nil {
		return 0, err
	}

	return len(data), nil
}
