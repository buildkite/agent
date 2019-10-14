package process

import (
	"bytes"
	"io"
)

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

	// loop through newlines
	for offset < len(data) {
		next := bytes.IndexRune(data[offset:], '\n')
		if next == -1 {
			break
		}
		out = append(out, data[offset:offset+next+1]...)
		out = append(out, []byte(p.f())...)
		offset = offset + next + 1
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
