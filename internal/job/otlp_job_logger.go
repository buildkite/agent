package job

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/buildkite/agent/v3/version"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	otellog "go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
)

// Keep newline-free output bounded while retaining normal line-oriented
// records. This matches the maximum chunk size used by common log shippers and
// avoids buffering an arbitrarily large process write until command exit.
const otlpLogRecordMaxBytes = 64 * 1024

// otlpJobLogger exports job log output as OpenTelemetry log records. It is fed
// already-redacted bytes from the two tee points downstream of the job's
// redactors (child-process output and shell logger control output), so the
// exported records carry the same [REDACTED] markers as the customer-facing
// job log without a second redaction pass.
//
// Log records are correlated with the job's trace: the executor calls
// setSpanContext at span boundaries (phases and hooks), and every record
// emitted until the next boundary carries that span's context (when tracing
// is enabled).
type otlpJobLogger struct {
	log      otellog.Logger
	provider *sdklog.LoggerProvider

	baseAttrs []otellog.KeyValue

	// mu guards the current emit context.
	mu sync.Mutex
	// ctx is stored, despite the general rule against keeping contexts in
	// structs (https://go.dev/blog/context-and-structs), because lines reach
	// the emitters through io.Writer, which cannot carry a per-write context.
	// It is used only as the carrier of the current span context for
	// stamping records at emit time: setSpanContext detaches it from
	// cancellation and deadlines, so no lifetime is smuggled in with it.
	ctx context.Context

	// output emits child-process output (hook/command stdout and stderr).
	output *otlpLineEmitter
	// control emits bootstrap control output (section headers, prompts,
	// comments, warnings) so the exported records match the Buildkite job log.
	control *otlpLineEmitter
}

func newOTLPJobLogger(ctx context.Context, e *Executor) (*otlpJobLogger, error) {
	protocol := os.Getenv("OTEL_EXPORTER_OTLP_LOGS_PROTOCOL")
	if protocol == "" {
		protocol = os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL")
	}
	if protocol == "" {
		protocol = "grpc"
	}

	var exporter sdklog.Exporter
	var err error
	switch protocol {
	case "grpc":
		exporter, err = otlploggrpc.New(ctx)
	case "http/protobuf", "http":
		exporter, err = otlploghttp.New(ctx)
	default:
		return nil, fmt.Errorf("unsupported OTLP logs protocol: %s", protocol)
	}
	if err != nil {
		return nil, fmt.Errorf("creating OTLP log exporter: %w", err)
	}

	serviceName := e.TracingServiceName
	if serviceName == "" {
		serviceName = "buildkite-agent"
	}
	resources := resource.NewWithAttributes(semconv.SchemaURL,
		semconv.ServiceNameKey.String(serviceName),
		semconv.ServiceVersionKey.String(version.Version()),
		semconv.DeploymentEnvironmentKey.String("ci"),
	)
	provider := sdklog.NewLoggerProvider(
		// Job logs must not be silently dropped when the SDK batch queue fills.
		// Back-pressure the command output on the exporter instead.
		sdklog.WithProcessor(sdklog.NewSimpleProcessor(exporter)),
		sdklog.WithResource(resources),
	)

	return newOTLPJobLoggerWithLogger(ctx, provider.Logger(
		"buildkite-agent",
		otellog.WithInstrumentationVersion(version.Version()),
		otellog.WithSchemaURL(semconv.SchemaURL),
	), provider, otlpJobAttributes(e)), nil
}

// newOTLPJobLoggerWithLogger assembles an otlpJobLogger from already-built
// parts. The span context in ctx (the root job span, when tracing is enabled)
// becomes the base emit context that records fall back to outside any
// setSpanContext boundary.
func newOTLPJobLoggerWithLogger(ctx context.Context, log otellog.Logger, provider *sdklog.LoggerProvider, baseAttrs []otellog.KeyValue) *otlpJobLogger {
	l := &otlpJobLogger{
		log:       log,
		provider:  provider,
		baseAttrs: baseAttrs,
		ctx:       context.WithoutCancel(ctx),
	}
	l.output = &otlpLineEmitter{logger: l}
	l.control = &otlpLineEmitter{logger: l}
	return l
}

// setSpanContext makes ctx the emit context for subsequent log records, and
// returns a function that restores the previous context when the span ends,
// so nested boundaries (a hook inside a phase) unwind correctly. Both
// emitters are flushed at each transition so buffered partial lines are
// attributed to the span that produced them. The stored context is detached
// from cancellation, preserving only trace context, so records emitted during
// cleanup do not inherit a canceled context.
func (l *otlpJobLogger) setSpanContext(ctx context.Context) func() {
	set := func(ctx context.Context) context.Context {
		l.output.flush()
		l.control.flush()
		l.mu.Lock()
		defer l.mu.Unlock()
		prevCtx := l.ctx
		l.ctx = ctx
		return prevCtx
	}

	prevCtx := set(context.WithoutCancel(ctx))
	return func() { set(prevCtx) }
}

// emitContext returns the current emit context and attributes.
func (l *otlpJobLogger) emitContext() (context.Context, []otellog.KeyValue) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.ctx, l.baseAttrs
}

// outputWriter accepts already-redacted child-process output.
func (l *otlpJobLogger) outputWriter() io.Writer { return l.output }

// controlWriter accepts already-redacted bootstrap control output.
func (l *otlpJobLogger) controlWriter() io.Writer { return l.control }

func (l *otlpJobLogger) Close(ctx context.Context) error {
	l.output.flush()
	l.control.flush()
	if l.provider == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	flushErr := l.provider.ForceFlush(ctx)
	shutdownErr := l.provider.Shutdown(ctx)
	if flushErr != nil {
		return flushErr
	}
	return shutdownErr
}

// otlpLineEmitter line-buffers (already redacted) output and emits each line as
// an OpenTelemetry log record with a native timestamp. Write and flush are
// guarded by mu because output may be written from multiple goroutines (e.g.
// the cancellation handler emitting a comment).
type otlpLineEmitter struct {
	logger *otlpJobLogger
	mu     sync.Mutex
	buf    []byte
	// lineStart is when the first byte of the buffered line arrived. Zero
	// while the buffer is empty.
	lineStart time.Time
}

func (e *otlpLineEmitter) Write(data []byte) (int, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	origLen := len(data)
	for len(data) > 0 {
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			e.appendChunks(data)
			return origLen, nil
		}

		emittedChunk := e.appendChunks(data[:i])
		e.buf = bytes.TrimSuffix(e.buf, []byte{'\r'})
		if len(e.buf) > 0 || !emittedChunk {
			e.emit(string(e.buf))
		}
		e.buf = e.buf[:0]
		data = data[i+1:]
	}

	return origLen, nil
}

func (e *otlpLineEmitter) appendChunks(data []byte) bool {
	emitted := false
	for len(data) > 0 {
		if len(e.buf) == 0 {
			e.lineStart = time.Now()
		}
		// Keep one full chunk buffered until we see either more data or a line
		// terminator. This lets Write strip a trailing carriage return when a
		// CRLF sequence arrives across separate writes without exceeding the
		// memory bound.
		if len(e.buf) == otlpLogRecordMaxBytes {
			e.emit(string(e.buf))
			e.buf = e.buf[:0]
			emitted = true
		}
		remaining := otlpLogRecordMaxBytes - len(e.buf)
		if remaining > len(data) {
			remaining = len(data)
		}
		e.buf = append(e.buf, data[:remaining]...)
		data = data[remaining:]
	}
	return emitted
}

func (e *otlpLineEmitter) flush() {
	e.mu.Lock()
	defer e.mu.Unlock()

	if len(e.buf) == 0 {
		return
	}
	e.emit(string(e.buf))
	e.buf = e.buf[:0]
}

func (e *otlpLineEmitter) emit(line string) {
	ctx, attrs := e.logger.emitContext()
	now := time.Now()

	// Stamp the record with the start of the line, matching the ANSI
	// timestamper's start-of-line semantics in the Buildkite job log as
	// closely as we can. It cannot match exactly: the timestamper also
	// injects mid-line timestamps for slow lines, which a single log record
	// cannot represent, and bytes withheld by the redactors reach this
	// emitter later than they were produced.
	start := e.lineStart
	e.lineStart = time.Time{}
	if start.IsZero() {
		start = now
	}

	var record otellog.Record
	record.SetTimestamp(start)
	record.SetObservedTimestamp(now)
	record.SetSeverity(otellog.SeverityInfo)
	record.SetSeverityText("INFO")
	record.SetBody(otellog.StringValue(line))
	record.AddAttributes(attrs...)
	e.logger.log.Emit(ctx, record)
}

func otlpJobAttributes(e *Executor) []otellog.KeyValue {
	buildID, _ := e.shell.Env.Get("BUILDKITE_BUILD_ID")
	buildNumber, _ := e.shell.Env.Get("BUILDKITE_BUILD_NUMBER")
	branch, _ := e.shell.Env.Get("BUILDKITE_BRANCH")
	label, _ := e.shell.Env.Get("BUILDKITE_LABEL")
	stepKey, _ := e.shell.Env.Get("BUILDKITE_STEP_KEY")
	agentID, _ := e.shell.Env.Get("BUILDKITE_AGENT_ID")

	return []otellog.KeyValue{
		otellog.String("source", "job"),
		otellog.String("buildkite.organization.slug", e.OrganizationSlug),
		otellog.String("buildkite.pipeline.slug", e.PipelineSlug),
		otellog.String("buildkite.branch", branch),
		otellog.String("buildkite.queue", e.Queue),
		otellog.String("buildkite.agent", e.AgentName),
		otellog.String("buildkite.agent.id", agentID),
		otellog.String("buildkite.build.id", buildID),
		otellog.String("buildkite.build.number", buildNumber),
		otellog.String("buildkite.job.id", e.JobID),
		otellog.String("buildkite.job.label", label),
		otellog.String("buildkite.job.key", stepKey),
	}
}

// setupOTLPJobLogger creates the OTLP job log exporter and mirrors the
// redacted job log streams into it. It returns a cleanup function that
// detaches the mirrors and flushes the exporter. The span context in ctx
// (the root job span, when tracing is enabled) becomes the base emit context
// that records fall back to outside any otlpLogSpan boundary.
func (e *Executor) setupOTLPJobLogger(ctx context.Context) func() {
	jobLogger, err := newOTLPJobLogger(ctx, e)
	if err != nil {
		e.shell.Warningf("Failed to initialize OTLP job log exporter: %v", err)
		return func() {}
	}

	// Mirror the redacted child-process output and shell logger control output
	// (section headers, prompts, comments, warnings) into OTLP so the exported
	// records match the downloadable Buildkite job log and the UI stream.
	e.otlpJobLogger = jobLogger
	if e.stdoutTee != nil {
		e.stdoutTee.setSecondary(jobLogger.outputWriter())
	}
	if e.stderrTee != nil {
		e.stderrTee.setSecondary(jobLogger.controlWriter())
	}

	return func() {
		if e.stdoutTee != nil {
			e.stdoutTee.setSecondary(nil)
		}
		if e.stderrTee != nil {
			e.stderrTee.setSecondary(nil)
		}
		if err := jobLogger.Close(ctx); err != nil {
			e.shell.Warningf("Failed to close OTLP job log exporter: %v", err)
		}
		e.otlpJobLogger = nil
	}
}

// otlpLogSpan records ctx as the current span context for OTLP job log
// records and returns a function that restores the previous context. Records
// emitted while it is current carry its span context (when tracing is
// enabled). No-op when OTLP job log export is disabled.
//
// Intended usage is a single statement right after starting a span:
//
//	defer e.otlpLogSpan(ctx)()
//
// otlpLogSpan runs immediately, installing the context before the function
// body produces any output; only the returned restore function is deferred,
// so nested boundaries (a hook inside a phase) unwind correctly on every
// return path. Records are stamped with the current context at emit time, so
// the install must precede any output. Registering the restore after the
// span's FinishWithError defer means it runs first (LIFO), flushing buffered
// partial lines while their span is still current.
func (e *Executor) otlpLogSpan(ctx context.Context) func() {
	if e.otlpJobLogger == nil {
		return func() {}
	}
	return e.otlpJobLogger.setSpanContext(ctx)
}
