package process

import (
	"errors"
	"io"
	"sync"
)

// ErrAlreadyClosed is returned when a buffer has already been closed.
var ErrAlreadyClosed = errors.New("already closed")

// Buffer implements a concurrent-safe output buffer for processes.
type Buffer struct {
	mu     sync.Mutex
	buf    []byte
	closed bool
}

// Write appends data to the buffer. If the buffer is closed, it returns
// io.ErrClosedPipe.
func (l *Buffer) Write(b []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return 0, io.ErrClosedPipe
	}
	l.buf = append(l.buf, b...)
	return len(b), nil
}

// Close closes the buffer. Further writes will return io.ErrClosedPipe.
func (l *Buffer) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return ErrAlreadyClosed
	}
	l.closed = true
	return nil
}

// ReadAndTruncate reads the unread contents of the buffer, and then truncates
// (empties) the buffer.
func (l *Buffer) ReadAndTruncate() []byte {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.buf) == 0 {
		return nil
	}
	// Return the current buf, but put a new empty buf of the same capacity in
	// its place. #IndianaJonesSwitchMeme
	b := l.buf
	l.buf = make([]byte, 0, cap(l.buf))
	return b
}
