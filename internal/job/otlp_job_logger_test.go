package job

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/buildkite/agent/v3/internal/redact"
	"github.com/buildkite/agent/v3/internal/replacer"
	otellog "go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/log/embedded"
)

// captureLogger is a minimal otellog.Logger that records emitted record bodies.
type captureLogger struct {
	embedded.Logger
	mu      sync.Mutex
	bodies  []string
	lastKVs map[string]string
}

func (c *captureLogger) Emit(_ context.Context, r otellog.Record) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.bodies = append(c.bodies, r.Body().AsString())
	c.lastKVs = map[string]string{}
	r.WalkAttributes(func(kv otellog.KeyValue) bool {
		c.lastKVs[string(kv.Key)] = kv.Value.AsString()
		return true
	})
}

func (c *captureLogger) Enabled(context.Context, otellog.EnabledParameters) bool { return true }

func newTestOTLPJobLogger(log otellog.Logger, needles ...string) *otlpJobLogger {
	mux := replacer.NewMux(replacer.New(nil, needles, redact.Redacted))
	return &otlpJobLogger{
		log:       log,
		attrs:     []otellog.KeyValue{otellog.String("source", "job")},
		redactors: mux,
	}
}

// TestOTLPJobLoggerRedactsSecrets ensures OTLP log records never carry secret
// values, matching the redaction applied to the customer-facing job log.
func TestOTLPJobLoggerRedactsSecrets(t *testing.T) {
	t.Parallel()

	const secret = "supersekret-value"
	cap := &captureLogger{}
	l := newTestOTLPJobLogger(cap, secret)

	var downstream bytes.Buffer
	w := l.Wrap(t.Context(), &downstream, map[string]string{"buildkite.phase": "command"})

	if _, err := w.Write([]byte("before " + secret + " after\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if f, ok := w.(interface{ Flush() }); ok {
		f.Flush()
	}

	cap.mu.Lock()
	defer cap.mu.Unlock()

	if len(cap.bodies) != 1 {
		t.Fatalf("emitted %d records, want 1: %q", len(cap.bodies), cap.bodies)
	}
	body := cap.bodies[0]
	if strings.Contains(body, secret) {
		t.Errorf("OTLP record body leaked secret: %q", body)
	}
	if !strings.Contains(body, "[REDACTED]") {
		t.Errorf("OTLP record body = %q, want it to contain [REDACTED]", body)
	}
	if got := cap.lastKVs["buildkite.phase"]; got != "command" {
		t.Errorf("buildkite.phase attribute = %q, want %q", got, "command")
	}
}

// TestOTLPJobLoggerControlWriter ensures bootstrap control output (section
// headers, prompts, comments) written through the control writer is emitted as
// OTLP log records, giving parity with the downloadable Buildkite job log.
func TestOTLPJobLoggerControlWriter(t *testing.T) {
	t.Parallel()

	cap := &captureLogger{}
	l := newTestOTLPJobLogger(cap)

	w := l.controlWriter()
	if _, err := w.Write([]byte("~~~ Running commands\n$ echo hello\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	// A line without a trailing newline should only be emitted on Close/flush.
	if _, err := w.Write([]byte("# trailing comment")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := l.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	cap.mu.Lock()
	defer cap.mu.Unlock()

	want := []string{"~~~ Running commands", "$ echo hello", "# trailing comment"}
	if len(cap.bodies) != len(want) {
		t.Fatalf("emitted %d records, want %d: %q", len(cap.bodies), len(want), cap.bodies)
	}
	for i, line := range want {
		if cap.bodies[i] != line {
			t.Errorf("record[%d] = %q, want %q", i, cap.bodies[i], line)
		}
	}
}

// TestOTLPJobLoggerControlWriterReused ensures repeated controlWriter calls
// return the same emitter so all control output shares one line buffer.
func TestOTLPJobLoggerControlWriterReused(t *testing.T) {
	t.Parallel()

	l := newTestOTLPJobLogger(&captureLogger{})
	if l.controlWriter() != l.controlWriter() {
		t.Error("controlWriter() returned different writers; want a single shared emitter")
	}
}

// TestOTLPJobLoggerRedactsSecretsSplitAcrossWrites ensures a secret that is
// split across multiple Write calls is still redacted in the OTLP output.
func TestOTLPJobLoggerRedactsSecretsSplitAcrossWrites(t *testing.T) {
	t.Parallel()

	const secret = "supersekret-value"
	cap := &captureLogger{}
	l := newTestOTLPJobLogger(cap, secret)

	w := l.Wrap(t.Context(), &bytes.Buffer{}, nil)

	// Split the secret across writes and withhold the trailing newline so the
	// line is only emitted on Flush.
	if _, err := w.Write([]byte("start super")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if _, err := w.Write([]byte("sekret-value end\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if f, ok := w.(interface{ Flush() }); ok {
		f.Flush()
	}

	cap.mu.Lock()
	defer cap.mu.Unlock()

	if len(cap.bodies) != 1 {
		t.Fatalf("emitted %d records, want 1: %q", len(cap.bodies), cap.bodies)
	}
	if body := cap.bodies[0]; strings.Contains(body, secret) {
		t.Errorf("OTLP record body leaked secret split across writes: %q", body)
	}
}
