package process

import (
	"cmp"
	"io"
	"strconv"
	"strings"
	"sync"

	"github.com/buildkite/osc7501"
)

// maxProgramStatusRecords bounds how many records ProgramStatusWriter
// remembers for skipping repeated reports. It matches the per-terminal record
// cap in the OSC 7501 spec.
const maxProgramStatusRecords = 256

// ProgramStatusWriter passes output through unchanged and, after each OSC 7501
// program status report in it, writes a line describing the report. Terminals
// and the Buildkite log viewer hide the report itself, so the line makes it
// visible in the job log.
//
// Reports that repeat the previous report for the same record, or only change
// its progress, don't produce a line.
//
// See https://www.superlogical.com/rex/docs/build/program-status
type ProgramStatusWriter struct {
	mu      sync.Mutex
	out     lineTracker
	scanner osc7501.Scanner
	logged  map[string]osc7501.Report // last report described, by record id
	lineErr error                     // from writing a report's line
}

// NewProgramStatusWriter returns a ProgramStatusWriter that writes to out.
func NewProgramStatusWriter(out io.Writer) *ProgramStatusWriter {
	w := &ProgramStatusWriter{
		out:    lineTracker{w: out, last: '\n'},
		logged: make(map[string]osc7501.Report),
	}
	w.scanner = osc7501.Scanner{W: &w.out, OnReport: w.describe}
	return w
}

// Write implements io.Writer.
func (w *ProgramStatusWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n, err := w.scanner.Write(p)
	err, w.lineErr = cmp.Or(err, w.lineErr), nil
	return n, err
}

// describe is called by the scanner after a report has been written to out.
func (w *ProgramStatusWriter) describe(r osc7501.Report) {
	if !w.remember(r) {
		return
	}

	var line strings.Builder
	if w.out.last != '\n' {
		// The report arrived mid-line, e.g. during a progress bar.
		line.WriteByte('\n')
	}
	line.WriteString("\x1b[90m# Program status: state=" + r.State)
	if r.ID != "" {
		line.WriteString(" id=" + r.ID)
	}
	if r.Kind != "" {
		line.WriteString(" kind=" + r.Kind)
	}
	if r.Progress >= 0 && (r.State == osc7501.StateWorking || r.State == osc7501.StateBlocked) {
		line.WriteString(" progress=" + strconv.Itoa(r.Progress))
	}
	if r.App != "" {
		line.WriteString(" app=" + r.App)
	}
	// Title and message are decoded program output; quoting escapes any
	// invisible formatting characters, such as text direction overrides.
	if r.Title != "" {
		line.WriteString(" title=" + strconv.Quote(r.Title))
	}
	if r.Msg != "" {
		line.WriteString(" msg=" + strconv.Quote(r.Msg))
	}
	line.WriteString("\x1b[0m\n")

	// The scanner can't return errors from this callback, so Write does.
	if _, err := io.WriteString(&w.out, line.String()); err != nil {
		w.lineErr = err
	}
}

// remember records r as the latest report for its record and reports whether
// it differs from the previous one by more than progress.
func (w *ProgramStatusWriter) remember(r osc7501.Report) bool {
	key := r
	key.Progress = 0
	prev, seen := w.logged[r.ID]

	if r.State == osc7501.StateClear {
		// Clearing a record also clears the records beneath it, even if this
		// clear repeats the last one: they may have been reported since.
		for id := range w.logged {
			if id != r.ID && (r.ID == "" || strings.HasPrefix(id, r.ID+"/")) {
				delete(w.logged, id)
			}
		}
	}
	if seen && prev == key {
		return false
	}
	if len(w.logged) >= maxProgramStatusRecords {
		clear(w.logged)
	}
	w.logged[r.ID] = key
	return true
}

// lineTracker writes to w and remembers the last byte written outside an ANSI
// escape sequence, so a report's line can start on a new line if needed.
type lineTracker struct {
	w    io.Writer
	ansi ansiParser
	last byte
}

func (t *lineTracker) Write(p []byte) (int, error) {
	n, err := t.w.Write(p)
	for i := range n {
		inside := t.ansi.insideCode()
		_, _ = t.ansi.Write(p[i : i+1])
		if !inside && p[i] != 0x1b {
			t.last = p[i]
		}
	}
	return n, err
}
