package job

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/buildkite/agent/v4/env"
	"github.com/buildkite/agent/v4/internal/shell"
	otellog "go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/log/embedded"
	"go.opentelemetry.io/otel/trace"
)

// captureLogger is a minimal otellog.Logger that records emitted record
// bodies, attributes and emit-context span IDs.
type captureLogger struct {
	embedded.Logger
	mu         sync.Mutex
	bodies     []string
	spanIDs    []trace.SpanID
	timestamps []time.Time
	lastKVs    map[string]string
}

func (c *captureLogger) Emit(ctx context.Context, r otellog.Record) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.bodies = append(c.bodies, r.Body().AsString())
	c.spanIDs = append(c.spanIDs, trace.SpanContextFromContext(ctx).SpanID())
	c.timestamps = append(c.timestamps, r.Timestamp())
	c.lastKVs = map[string]string{}
	r.WalkAttributes(func(kv otellog.KeyValue) bool {
		c.lastKVs[string(kv.Key)] = kv.Value.AsString()
		return true
	})
}

func (c *captureLogger) Enabled(context.Context, otellog.EnabledParameters) bool { return true }

func newTestOTLPJobLogger(log otellog.Logger) *otlpJobLogger {
	return newOTLPJobLoggerWithLogger(context.Background(), log, nil, []otellog.KeyValue{otellog.String("source", "job")})
}

func spanContextWithID(t *testing.T, id string) context.Context {
	t.Helper()
	spanID, err := trace.SpanIDFromHex(id)
	if err != nil {
		t.Fatalf("trace.SpanIDFromHex(%q) error = %v", id, err)
	}
	traceID, err := trace.TraceIDFromHex("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatalf("trace.TraceIDFromHex() error = %v", err)
	}
	return trace.ContextWithSpanContext(t.Context(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID,
		SpanID:  spanID,
	}))
}

// TestOTLPJobLoggerEmitsRedactedOutput ensures OTLP log records mirror the
// child-process output after the job log redactor, so records never carry
// secret values, including secrets split across writes and secrets added
// mid-job (e.g. via the Job API).
func TestOTLPJobLoggerEmitsRedactedOutput(t *testing.T) {
	t.Parallel()

	const secret = "supersekret-value"
	e := New(ExecutorConfig{RedactedVars: []string{"MY_SECRET"}})
	environ := env.New()
	environ.Set("MY_SECRET", secret)

	var stdout, stderr bytes.Buffer
	preRedactedStdout, _ := e.setupRedactors(shell.DiscardLogger, environ, &stdout, &stderr)

	cap := &captureLogger{}
	l := newTestOTLPJobLogger(cap)
	e.otlpJobLogger = l
	e.stdoutTee.setSecondary(l.outputWriter())

	if _, err := preRedactedStdout.Write([]byte("before " + secret + " after\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	// A secret split across writes must also be redacted.
	if _, err := preRedactedStdout.Write([]byte("start super")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if _, err := preRedactedStdout.Write([]byte("sekret-value end\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	// A secret added mid-job (e.g. via the Job API) must be redacted too.
	const lateSecret = "late-bound-secret"
	e.redactors.Add(lateSecret)
	if _, err := preRedactedStdout.Write([]byte("value " + lateSecret + " end\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	cap.mu.Lock()
	defer cap.mu.Unlock()

	want := []string{"before [REDACTED] after", "start [REDACTED] end", "value [REDACTED] end"}
	if len(cap.bodies) != len(want) {
		t.Fatalf("emitted %d records, want %d: %q", len(cap.bodies), len(want), cap.bodies)
	}
	for i, line := range want {
		if cap.bodies[i] != line {
			t.Errorf("record[%d] = %q, want %q", i, cap.bodies[i], line)
		}
	}
	if got := stdout.String(); strings.Contains(got, secret) || strings.Contains(got, lateSecret) {
		t.Errorf("job log stdout leaked secret: %q", got)
	}
}

// TestOTLPJobLoggerSetSpanContext ensures records carry the current span
// context, that a buffered partial line is attributed to the span that
// produced it, and that the returned restore function unwinds nested spans.
func TestOTLPJobLoggerSetSpanContext(t *testing.T) {
	t.Parallel()

	cap := &captureLogger{}
	l := newTestOTLPJobLogger(cap)
	w := l.outputWriter()

	checkoutCtx := spanContextWithID(t, "bbbbbbbbbbbbbbbb")
	hookCtx := spanContextWithID(t, "cccccccccccccccc")

	restoreCheckout := l.setSpanContext(checkoutCtx)
	if _, err := w.Write([]byte("checkout line\npartial")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	// Switching spans flushes the buffered partial line with the old span.
	restoreHook := l.setSpanContext(hookCtx)
	if _, err := w.Write([]byte("hook line\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	// Restoring unwinds to the enclosing span.
	restoreHook()
	if _, err := w.Write([]byte("more checkout\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	restoreCheckout()

	cap.mu.Lock()
	defer cap.mu.Unlock()

	wantBodies := []string{"checkout line", "partial", "hook line", "more checkout"}
	if len(cap.bodies) != len(wantBodies) {
		t.Fatalf("emitted records = %q, want %q", cap.bodies, wantBodies)
	}
	for i, body := range wantBodies {
		if cap.bodies[i] != body {
			t.Errorf("record[%d] = %q, want %q", i, cap.bodies[i], body)
		}
	}
	checkoutSpanID := trace.SpanContextFromContext(checkoutCtx).SpanID()
	hookSpanID := trace.SpanContextFromContext(hookCtx).SpanID()
	wantSpanIDs := []trace.SpanID{checkoutSpanID, checkoutSpanID, hookSpanID, checkoutSpanID}
	for i, want := range wantSpanIDs {
		if cap.spanIDs[i] != want {
			t.Errorf("record[%d] span ID = %s, want %s", i, cap.spanIDs[i], want)
		}
	}
}

// TestOTLPLineEmitterStartOfLineTimestamp ensures a record is stamped with
// the arrival of the line's first byte rather than its completion, matching
// the ANSI timestamper's start-of-line semantics in the Buildkite job log.
func TestOTLPLineEmitterStartOfLineTimestamp(t *testing.T) {
	t.Parallel()

	cap := &captureLogger{}
	l := newTestOTLPJobLogger(cap)
	w := l.outputWriter()

	before := time.Now()
	if _, err := w.Write([]byte("slow line")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	after := time.Now()

	time.Sleep(20 * time.Millisecond)
	if _, err := w.Write([]byte(" done\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	cap.mu.Lock()
	defer cap.mu.Unlock()

	if len(cap.timestamps) != 1 {
		t.Fatalf("emitted %d records, want 1: %q", len(cap.timestamps), cap.bodies)
	}
	ts := cap.timestamps[0]
	if ts.Before(before) || ts.After(after) {
		t.Errorf("record timestamp = %v, want within [%v, %v]", ts, before, after)
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
	if err := l.Close(t.Context()); err != nil {
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
	if got := cap.lastKVs["source"]; got != "job" {
		t.Errorf("source attribute = %q, want %q", got, "job")
	}
}

func TestOTLPLineEmitterChunksUnterminatedOutput(t *testing.T) {
	t.Parallel()

	cap := &captureLogger{}
	l := newTestOTLPJobLogger(cap)
	w := l.outputWriter()
	line := strings.Repeat("x", otlpLogRecordMaxBytes*2+17)

	if _, err := w.Write([]byte(line)); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	cap.mu.Lock()
	if got, want := len(cap.bodies), 2; got != want {
		cap.mu.Unlock()
		t.Fatalf("records before flush = %d, want %d", got, want)
	}
	cap.mu.Unlock()

	l.output.flush()

	cap.mu.Lock()
	defer cap.mu.Unlock()
	if got, want := len(cap.bodies), 3; got != want {
		t.Fatalf("records after flush = %d, want %d", got, want)
	}
	if got := strings.Join(cap.bodies, ""); got != line {
		t.Errorf("rejoined record bodies differ from input: got %d bytes, want %d", len(got), len(line))
	}
}
