// Package logtest provides slog helpers for tests.
package logtest

import (
	"context"
	"log/slog"
	"sync"
)

// Handler records log records in memory and is safe for concurrent use.
type Handler struct {
	mu      *sync.Mutex
	records *[]slog.Record
	attrs   []slog.Attr
	groups  []string
}

// NewLogger returns a logger and its record-capturing handler.
func NewLogger() (*slog.Logger, *Handler) {
	records := make([]slog.Record, 0)
	h := &Handler{mu: new(sync.Mutex), records: &records}
	return slog.New(h), h
}

func (h *Handler) Enabled(context.Context, slog.Level) bool { return true }

func (h *Handler) Handle(_ context.Context, record slog.Record) error {
	captured := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
	if len(h.attrs) > 0 {
		captured.AddAttrs(h.attrs...)
	}
	attrs := make([]slog.Attr, 0, record.NumAttrs())
	record.Attrs(func(attr slog.Attr) bool {
		attrs = append(attrs, attr)
		return true
	})
	captured.AddAttrs(groupAttrs(h.groups, attrs)...)

	h.mu.Lock()
	*h.records = append(*h.records, captured)
	h.mu.Unlock()
	return nil
}

func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clone := *h
	clone.attrs = append(append([]slog.Attr(nil), h.attrs...), groupAttrs(h.groups, attrs)...)
	return &clone
}

func (h *Handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	clone := *h
	clone.groups = append(append([]string(nil), h.groups...), name)
	return &clone
}

func groupAttrs(groups []string, attrs []slog.Attr) []slog.Attr {
	for i := len(groups) - 1; i >= 0; i-- {
		attrs = []slog.Attr{{Key: groups[i], Value: slog.GroupValue(attrs...)}}
	}
	return attrs
}

// Records returns a snapshot of the captured records.
func (h *Handler) Records() []slog.Record {
	h.mu.Lock()
	defer h.mu.Unlock()

	records := make([]slog.Record, len(*h.records))
	copy(records, *h.records)
	return records
}

// Messages returns the captured record messages.
func (h *Handler) Messages() []string {
	records := h.Records()
	messages := make([]string, len(records))
	for i, record := range records {
		messages[i] = record.Message
	}
	return messages
}
