package logger

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
)

// Recorder captures slog.Records emitted to a logger created by Test
// so that tests can assert on log output.
//
// Methods are safe for concurrent use.
type Recorder struct {
	mu      sync.Mutex
	records []slog.Record
}

// Records returns a snapshot of the captured records.
func (r *Recorder) Records() []slog.Record {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.records)
}

// Messages returns the message of every captured record, in order.
func (r *Recorder) Messages() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.records))
	for i, rec := range r.records {
		out[i] = rec.Message
	}
	return out
}

// MessagesAtLevel returns the messages of records with the given
// level, in order.
func (r *Recorder) MessagesAtLevel(lvl slog.Level) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, rec := range r.records {
		if rec.Level == lvl {
			out = append(out, rec.Message)
		}
	}
	return out
}

// HasMessage reports whether any captured record's message contains
// the given substring.
func (r *Recorder) HasMessage(substr string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, rec := range r.records {
		if strings.Contains(rec.Message, substr) {
			return true
		}
	}
	return false
}

// TestOption configures a logger created by Test.
type TestOption func(*testHandler)

// QuietTb disables piping records through tb.Logf. Useful for tests
// that produce a lot of output, or that test logger configuration
// directly.
func QuietTb() TestOption {
	return func(h *testHandler) {
		h.quietTb = true
	}
}

// Test returns a *slog.Logger that records every record into a
// Recorder and (unless QuietTb is supplied) also writes a
// human-readable formatted record via tb.Logf so the output is
// visible on test failure.
func Test(tb testing.TB, opts ...TestOption) (*slog.Logger, *Recorder) {
	tb.Helper()
	rec := &Recorder{}
	h := &testHandler{
		tb:  tb,
		rec: rec,
	}
	for _, opt := range opts {
		opt(h)
	}
	return slog.New(h), rec
}

// testHandler is an slog.Handler that captures records into a
// Recorder and optionally writes them to testing.TB.Logf.
type testHandler struct {
	tb      testing.TB
	rec     *Recorder
	quietTb bool

	// attrs collected from With() calls. They are appended to every
	// record before it is recorded.
	attrs []slog.Attr

	// group is the dotted group name applied to attributes added via
	// later WithAttrs calls. Empty if no group is active.
	group string
}

func (h *testHandler) Enabled(_ context.Context, _ slog.Level) bool {
	return true
}

func (h *testHandler) Handle(_ context.Context, rec slog.Record) error {
	rec = rec.Clone()
	if len(h.attrs) > 0 {
		rec.AddAttrs(h.attrs...)
	}

	h.rec.mu.Lock()
	h.rec.records = append(h.rec.records, rec)
	h.rec.mu.Unlock()

	if !h.quietTb {
		h.tb.Helper()
		h.tb.Logf("%s", formatRecord(rec))
	}
	return nil
}

func (h *testHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	nh := *h
	nh.attrs = slices.Clone(h.attrs)
	if h.group == "" {
		nh.attrs = append(nh.attrs, attrs...)
	} else {
		// Wrap the attrs in the active group so keys are prefixed.
		anyAttrs := make([]any, len(attrs))
		for i, a := range attrs {
			anyAttrs[i] = a
		}
		nh.attrs = append(nh.attrs, slog.Group(h.group, anyAttrs...))
	}
	return &nh
}

func (h *testHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	nh := *h
	if h.group == "" {
		nh.group = name
	} else {
		nh.group = h.group + "." + name
	}
	return &nh
}

// formatRecord renders a record into a single line suitable for
// tb.Logf.
func formatRecord(rec slog.Record) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s %s", rec.Level, rec.Message)
	rec.Attrs(func(a slog.Attr) bool {
		fmt.Fprintf(&sb, " %s=%v", a.Key, a.Value.Any())
		return true
	})
	return sb.String()
}
