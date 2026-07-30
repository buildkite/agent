package job

import (
	"io"
	"sync"
)

// teeWriter writes to a primary writer and, when configured, also to a
// secondary writer. The secondary sink can be attached after construction,
// which lets the executor mirror already-redacted shell logger output into the
// OTLP exporter once it has been initialised.
type teeWriter struct {
	mu        sync.Mutex
	primary   io.Writer
	secondary io.Writer
}

func (w *teeWriter) Write(p []byte) (int, error) {
	// Hold the lock for the whole write so that detaching the secondary sink
	// (setSecondary(nil), called before the OTLP exporter is flushed and shut
	// down) cannot complete while a write is still in flight. This guarantees
	// no control output lands on the OTLP emitter after it has been flushed.
	w.mu.Lock()
	defer w.mu.Unlock()

	n, err := w.primary.Write(p)
	if err != nil || w.secondary == nil {
		return n, err
	}
	secondaryN, err := w.secondary.Write(p)
	if err == nil && secondaryN != len(p) {
		err = io.ErrShortWrite
	}
	return n, err
}

func (w *teeWriter) setSecondary(secondary io.Writer) {
	w.mu.Lock()
	w.secondary = secondary
	w.mu.Unlock()
}
