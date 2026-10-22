// Package joberrorosc extracts standard OSC 7501 errors from live job output.
package joberrorosc

import (
	"io"
	"sync"

	"github.com/buildkite/agent/v4/jobapi"
	"github.com/wolfeidau/osc7501"
)

// Code is a placeholder required by the backend API. Classifier transport is
// deliberately out of scope for this POC; customers only supply a plain Msg.
const Code = "buildkite.osc_error"

// Writer preserves log bytes and forwards a separately validated/redacted copy
// of each complete state=error report. Invalid reports and other states are
// ignored for error capture, not removed from the log.
type Writer struct {
	mu      sync.Mutex
	scanner osc7501.Scanner
}

// NewWriter uses the current secret needles on every report, including secrets
// registered after construction. onError runs synchronously and must not write
// back to this Writer. A report is an observation, not an authenticated identity.
func NewWriter(out io.Writer, needles func() []string, onError func(*jobapi.CapturedError)) *Writer {
	w := &Writer{}
	w.scanner = osc7501.Scanner{W: out, OnReport: func(r osc7501.Report) {
		if r.State != osc7501.StateError {
			return
		}
		e := &jobapi.CapturedError{Code: Code, Message: r.Msg}
		if _, err := e.Prepare(needles()); err != nil {
			return
		}
		onError(e)
	}}
	return w
}

func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.scanner.Write(p)
}
