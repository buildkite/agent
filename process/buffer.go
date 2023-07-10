package process

import "sync"

// Buffer implements a concurrent-safe output buffer for processes.
type Buffer struct {
	mu  sync.Mutex
	buf []byte
}

// Write appends data to the buffer.
func (l *Buffer) Write(b []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.buf = append(l.buf, b...)
	return len(b), nil
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
