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

// TestOTLPJobLoggerRedactsNeedlesAddedMidStream ensures a secret added to the
// live redactor Mux after the command writer is created (e.g. via the Job API)
// is still redacted in OTLP output, not just secrets known at writer creation.
func TestOTLPJobLoggerRedactsNeedlesAddedMidStream(t *testing.T) {
	t.Parallel()

	cap := &captureLogger{}
	// Start with no needles, matching a command that adds a secret at runtime.
	l := newTestOTLPJobLogger(cap)

	w := l.Wrap(t.Context(), &bytes.Buffer{}, nil)

	const secret = "late-bound-secret"
	// Secret added to the live Mux after the writer was created.
	l.redactors.Add(secret)

	if _, err := w.Write([]byte("value " + secret + " end\n")); err != nil {
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
		t.Errorf("OTLP record body leaked mid-stream secret: %q", body)
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

// TestOTLPJobLoggerRedactsSecretsSplitAcrossCommands ensures the OTLP redactor
// retains partial matches across wrappers for sequential commands that share
// the same downstream job-log stream.
func TestOTLPJobLoggerRedactsSecretsSplitAcrossCommands(t *testing.T) {
	t.Parallel()

	const secret = "supersekret-value"
	cap := &captureLogger{}
	l := newTestOTLPJobLogger(cap, secret)
	var downstream bytes.Buffer

	first := l.Wrap(t.Context(), &downstream, map[string]string{"buildkite.phase": "checkout"})
	if _, err := first.Write([]byte("start super")); err != nil {
		t.Fatalf("first Write() error = %v", err)
	}
	if f, ok := first.(interface{ Flush() }); ok {
		f.Flush()
	}

	second := l.Wrap(t.Context(), &downstream, map[string]string{"buildkite.phase": "command"})
	if _, err := second.Write([]byte("sekret-value end\n")); err != nil {
		t.Fatalf("second Write() error = %v", err)
	}
	if f, ok := second.(interface{ Flush() }); ok {
		f.Flush()
	}
	if err := l.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	cap.mu.Lock()
	defer cap.mu.Unlock()
	body := strings.Join(cap.bodies, "")
	if strings.Contains(body, secret) || strings.Contains(body, "super") || strings.Contains(body, "sekret-value") {
		t.Errorf("OTLP records leaked secret fragments across commands: %q", cap.bodies)
	}
	if !strings.Contains(body, "[REDACTED]") {
		t.Errorf("OTLP records = %q, want them to contain [REDACTED]", cap.bodies)
	}
	if got := downstream.String(); got != "start "+secret+" end\n" {
		t.Errorf("downstream = %q, want unmodified command output", got)
	}
}

// TestOTLPJobLoggerFlushRedactorsMatchesExecutorBoundary ensures an explicit
// executor redactor flush ends a partial match and emits it using the phase
// that produced it, while ordinary per-command flushes do not.
func TestOTLPJobLoggerFlushRedactorsMatchesExecutorBoundary(t *testing.T) {
	t.Parallel()

	const secret = "supersekret-value"
	cap := &captureLogger{}
	l := newTestOTLPJobLogger(cap, secret)
	var downstream bytes.Buffer

	first := l.Wrap(t.Context(), &downstream, map[string]string{"buildkite.phase": "hook"})
	if _, err := first.Write([]byte("start super")); err != nil {
		t.Fatalf("first Write() error = %v", err)
	}
	if f, ok := first.(interface{ Flush() }); ok {
		f.Flush()
	}
	l.FlushRedactors()

	second := l.Wrap(t.Context(), &downstream, map[string]string{"buildkite.phase": "command"})
	if _, err := second.Write([]byte("sekret-value end\n")); err != nil {
		t.Fatalf("second Write() error = %v", err)
	}
	if f, ok := second.(interface{ Flush() }); ok {
		f.Flush()
	}

	cap.mu.Lock()
	defer cap.mu.Unlock()
	// The first command flush has already emitted the safe prefix. The
	// executor boundary then releases the withheld partial match before the
	// next phase starts.
	want := []string{"start ", "super", "sekret-value end"}
	if len(cap.bodies) != len(want) {
		t.Fatalf("emitted records = %q, want %q", cap.bodies, want)
	}
	for i := range want {
		if cap.bodies[i] != want[i] {
			t.Errorf("record[%d] = %q, want %q", i, cap.bodies[i], want[i])
		}
	}
}

func TestOTLPJobLoggerChunksUnterminatedOutput(t *testing.T) {
	t.Parallel()

	cap := &captureLogger{}
	l := newTestOTLPJobLogger(cap)
	w := l.Wrap(t.Context(), &bytes.Buffer{}, nil)
	line := strings.Repeat("x", otlpLogRecordMaxBytes*2+17)

	if _, err := w.Write([]byte(line)); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	cap.mu.Lock()
	if got, want := len(cap.bodies), 2; got != want {
		cap.mu.Unlock()
		t.Fatalf("records before Flush = %d, want %d", got, want)
	}
	cap.mu.Unlock()

	if f, ok := w.(interface{ Flush() }); ok {
		f.Flush()
	}

	cap.mu.Lock()
	defer cap.mu.Unlock()
	if got, want := len(cap.bodies), 3; got != want {
		t.Fatalf("records after Flush = %d, want %d", got, want)
	}
	if got := strings.Join(cap.bodies, ""); got != line {
		t.Errorf("rejoined record bodies differ from input: got %d bytes, want %d", len(got), len(line))
	}
}
