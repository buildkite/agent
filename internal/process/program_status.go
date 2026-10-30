package process

import (
	"cmp"
	"io"
	"strconv"
	"strings"
	"sync"

	"github.com/buildkite/agent/v4/internal/redact"
	"github.com/buildkite/agent/v4/internal/replacer"
	"github.com/buildkite/osc7501"
)

// maxProgramStatusRecords bounds how many records ProgramStatusWriter
// remembers for skipping repeated reports. It matches the per-terminal record
// cap in the OSC 7501 spec.
const maxProgramStatusRecords = 256

// ProgramStatusWriter redacts and re-encodes each OSC 7501 report and adds a readable line.
// Terminals and the Buildkite log viewer hide the report itself, so the line
// makes it visible in the job log.
//
// Reports that repeat the previous report for the same record, or only change
// its progress, don't produce a line.
//
// See https://www.superlogical.com/rex/docs/build/program-status
type ProgramStatusWriter struct {
	mu           sync.Mutex
	out          lineTracker
	scanner      osc7501.Scanner
	logged       map[string]osc7501.Report // last report described, by record id
	lineErr      error                     // from writing a report's line
	redactors    *replacer.Mux
	filtered     *programStatusOutput
	redactedText strings.Builder
	textRedactor *replacer.Replacer
}

// NewProgramStatusWriter replaces complete OSC reports with their decoded,
// redacted and re-encoded form before they reach logs or other consumers.
// Unknown fields and complete invalid reports are omitted rather than forwarded.
func NewProgramStatusWriter(out io.Writer, redactors *replacer.Mux) *ProgramStatusWriter {
	w := &ProgramStatusWriter{
		out:       lineTracker{w: out, last: '\n'},
		logged:    make(map[string]osc7501.Report),
		redactors: redactors,
	}
	w.filtered = &programStatusOutput{out: &w.out}
	w.scanner = osc7501.Scanner{W: w.filtered, OnReport: w.describe}
	w.textRedactor = redact.New(&w.redactedText, nil)
	w.textRedactor.AddPrefixes(redact.TokenPrefixes()...)
	return w
}

// Flush releases an incomplete trailing escape sequence without dropping log bytes.
func (w *ProgramStatusWriter) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, err := w.out.Write(w.filtered.stripper.Flush())
	return err
}

// Write implements io.Writer.
func (w *ProgramStatusWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n, err := w.scanner.Write(p)
	err, w.lineErr = cmp.Or(err, w.lineErr), nil
	return n, err
}

// describe is called after the original frame has been stripped from the output.
func (w *ProgramStatusWriter) describe(r osc7501.Report) {
	w.textRedactor.Reset(redact.AppendGoEscaped(w.redactors.Needles()))
	for _, text := range []*string{&r.Title, &r.Msg} {
		w.redactedText.Reset()
		_, _ = io.WriteString(w.textRedactor, *text)
		_ = w.textRedactor.Flush()
		*text = w.redactedText.String()
	}
	if _, err := w.out.Write(r.Encode()); err != nil {
		w.lineErr = err
		return
	}
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

type programStatusOutput struct {
	out      *lineTracker
	stripper osc7501.Stripper
}

func (o *programStatusOutput) Write(p []byte) (int, error) {
	if _, err := o.out.Write(o.stripper.Strip(p)); err != nil {
		return 0, err
	}
	return len(p), nil
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
