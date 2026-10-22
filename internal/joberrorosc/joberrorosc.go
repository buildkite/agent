// Package joberrorosc transports captured errors in a private, versioned
// convention over OSC 7501. It is not part of the program-status specification.
package joberrorosc

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/buildkite/agent/v4/api"
	"github.com/wolfeidau/osc7501"
)

const (
	app    = "buildkite-job-error"
	prefix = "job-error/v1/"
	// Msg is limited to 2048 decoded bytes, not characters. Base64 inside
	// Msg keeps arbitrary JSON (including controls and Unicode) unchanged by
	// the library's text sanitization. Chunk before its silent truncation.
	chunkSize  = 2000
	maxParts   = 16
	maxPending = 16
)

// Write emits an already-redacted, normalized error. Callers must serialize
// concurrent writes to w, just as for other OSC reports.
func Write(w io.Writer, e api.JobCapturedError) error {
	if err := validate(e); err != nil {
		return err
	}
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	encoded := base64.StdEncoding.EncodeToString(b)
	parts := (len(encoded) + chunkSize - 1) / chunkSize
	if parts > maxParts {
		return errors.New("captured error exceeds OSC envelope limit")
	}
	reporter := osc7501.NewReporter(w, app)
	for i := range parts {
		if err := reporter.Report(osc7501.Report{
			State: osc7501.StateError,
			ID:    prefix + e.IdempotencyKey,
			Title: fmt.Sprintf("%d/%d", i+1, parts),
			Msg:   encoded[i*chunkSize : min((i+1)*chunkSize, len(encoded))],
		}); err != nil {
			return err
		}
	}
	return nil
}

type partial struct {
	parts int
	next  int
	data  string
}

// Writer passes logs through unchanged and extracts complete error envelopes.
// It handles arbitrarily split writes and serializes concurrent writers.
// onError runs synchronously; it must not write back to this Writer.
type Writer struct {
	mu      sync.Mutex
	scanner osc7501.Scanner
	pending map[string]partial
	onError func(api.JobCapturedError)
}

// NewWriter creates a log writer. Invalid, incomplete, unrelated and future
// version reports remain in the log but never reach onError. Assembly memory
// is bounded even when a job writes malicious or unfinished envelopes.
func NewWriter(out io.Writer, onError func(api.JobCapturedError)) *Writer {
	w := &Writer{pending: make(map[string]partial), onError: onError}
	w.scanner = osc7501.Scanner{W: out, OnReport: w.report}
	return w
}

func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.scanner.Write(p)
}

func (w *Writer) report(r osc7501.Report) {
	if r.App != app || r.State != osc7501.StateError || !strings.HasPrefix(r.ID, prefix) {
		return
	}
	index, total, ok := strings.Cut(r.Title, "/")
	i, errI := strconv.Atoi(index)
	n, errN := strconv.Atoi(total)
	if !ok || errI != nil || errN != nil || i < 1 || i > n || n > maxParts || len(r.Msg) > chunkSize {
		delete(w.pending, r.ID)
		return
	}
	p := w.pending[r.ID]
	if i == 1 {
		if len(w.pending) >= maxPending {
			clear(w.pending)
		}
		p = partial{parts: n, next: 1}
	}
	delete(w.pending, r.ID)
	if p.next != i || p.parts != n {
		return
	}
	p.data += r.Msg
	if i < n {
		p.next++
		w.pending[r.ID] = p
		return
	}
	b, err := base64.StdEncoding.DecodeString(p.data)
	if err != nil {
		return
	}
	var e api.JobCapturedError
	if json.Unmarshal(b, &e) != nil || validate(e) != nil || prefix+e.IdempotencyKey != r.ID {
		return
	}
	w.onError(e)
}

func validate(e api.JobCapturedError) error {
	if strings.TrimSpace(e.Code) == "" || len(e.Code) > 255 || strings.ContainsRune(e.Code, 0) || !utf8.ValidString(e.Code) ||
		strings.TrimSpace(e.Message) == "" || strings.ContainsRune(e.Message, 0) || !utf8.ValidString(e.Message) || utf8.RuneCountInString(e.Message) > 1000 ||
		e.Timestamp.UTC().Year() < 1 || e.Timestamp.UTC().Year() > 9999 || !osc7501.ValidSegment(e.IdempotencyKey) {
		return errors.New("invalid captured error OSC envelope")
	}
	return nil
}
