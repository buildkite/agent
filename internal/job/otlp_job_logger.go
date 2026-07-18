package job

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/buildkite/agent/v3/internal/redact"
	"github.com/buildkite/agent/v3/internal/replacer"
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

type otlpJobLogger struct {
	log       otellog.Logger
	provider  *sdklog.LoggerProvider
	attrs     []otellog.KeyValue
	redactors *replacer.Mux
	streamsMu sync.Mutex
	streams   map[io.Writer]*otlpJobLogStream

	// control mirrors bootstrap-generated control output (section headers,
	// prompts, comments, warnings) into OTLP so the exported records match the
	// Buildkite job log stream. It is created lazily by controlWriter.
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

	return &otlpJobLogger{
		log: provider.Logger(
			"buildkite-agent",
			otellog.WithInstrumentationVersion(version.Version()),
			otellog.WithSchemaURL(semconv.SchemaURL),
		),
		provider:  provider,
		attrs:     otlpJobAttributes(e),
		redactors: e.redactors,
	}, nil
}

func (l *otlpJobLogger) Wrap(ctx context.Context, out io.Writer, attrs map[string]string) io.Writer {
	emitter := &otlpLineEmitter{
		ctx:   context.WithoutCancel(ctx),
		log:   l.log,
		attrs: appendLogAttrs(l.attrs, attrs),
	}

	// Keep one redaction stream for each shared downstream job-log writer. The
	// visible job-log redactor also persists across command boundaries, so OTLP
	// must retain partial matches across Wrap calls to avoid leaking fragments
	// of a secret split between two commands. Executor output passes pointer-
	// based writers, so the io.Writer interface values are valid map keys.
	l.streamsMu.Lock()
	if l.streams == nil {
		l.streams = make(map[io.Writer]*otlpJobLogStream)
	}
	stream := l.streams[out]
	if stream == nil {
		stream = &otlpJobLogStream{out: out, live: l.redactors}
		stream.redactor = replacer.New(
			otlpRedactedWriter{stream: stream},
			l.redactors.Needles(),
			redact.Redacted,
		)
		l.streams[out] = stream
	}
	l.streamsMu.Unlock()

	return &otlpJobLogWriter{
		stream:  stream,
		emitter: emitter,
	}
}

// controlWriter returns an io.Writer that mirrors bootstrap control output
// (section headers, prompts, comments, warnings) into OTLP as log records, so
// the exported records contain the same lines a customer sees in the Buildkite
// UI. It is fed post-redaction bytes from the shell logger's redactor, so it
// does not redact again. Lines carry the base job attributes but no per-hook
// phase or span context (control output is bootstrap narration, not the output
// of a specific traced hook/command).
func (l *otlpJobLogger) controlWriter() io.Writer {
	if l.control == nil {
		l.control = &otlpLineEmitter{
			ctx:   context.Background(),
			log:   l.log,
			attrs: l.attrs,
		}
	}
	return l.control
}

func (l *otlpJobLogger) Close(ctx context.Context) error {
	if l.control != nil {
		l.control.flush()
	}
	l.FlushRedactors()
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

// FlushRedactors ends the current shared OTLP redaction streams. The executor
// calls this at the same explicit hook/default-command boundaries where it
// flushes the customer-facing redactors. Individual subprocesses within one of
// those phases deliberately do not flush this state.
func (l *otlpJobLogger) FlushRedactors() {
	l.streamsMu.Lock()
	streams := make([]*otlpJobLogStream, 0, len(l.streams))
	for _, stream := range l.streams {
		streams = append(streams, stream)
	}
	l.streamsMu.Unlock()
	for _, stream := range streams {
		stream.flush()
	}
}

// otlpJobLogWriter tees process output to the normal (already redacting) job-log
// writer and to a redacting OTLP line emitter, so OTLP records carry the same
// redacted content as the customer-facing job log.
type otlpJobLogWriter struct {
	stream  *otlpJobLogStream
	emitter *otlpLineEmitter
}

func (w *otlpJobLogWriter) Write(data []byte) (int, error) {
	w.stream.mu.Lock()
	defer w.stream.mu.Unlock()

	n, err := w.stream.out.Write(data)
	// Keep the OTLP redactor's needles in sync with the live job redactor
	// before redacting, so secrets added mid-command (e.g. via the Job API)
	// are redacted in OTLP output too. Replacer.Add deduplicates, so re-adding
	// the full needle set each write is safe.
	if w.stream.live != nil {
		w.stream.redactor.Add(w.stream.live.Needles()...)
	}
	// Feed the OTLP copy through the redactor, which streams redacted bytes to
	// this command's line emitter. Errors here must not affect the primary job
	// log. The stream retains partial redaction matches for the next command.
	w.stream.emitter = w.emitter
	_, _ = w.stream.redactor.Write(data)
	return n, err
}

func (w *otlpJobLogWriter) Flush() {
	w.stream.mu.Lock()
	defer w.stream.mu.Unlock()
	// Flush this command's complete line data, but deliberately do not flush
	// the shared redactor: it may be withholding the prefix of a secret that
	// continues in the next command.
	w.emitter.flush()
}

// otlpJobLogStream owns redaction state shared by command writers that feed the
// same downstream job-log stream.
type otlpJobLogStream struct {
	mu       sync.Mutex
	out      io.Writer
	live     *replacer.Mux
	redactor *replacer.Replacer
	emitter  *otlpLineEmitter
}

func (s *otlpJobLogStream) flush() {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.redactor.Flush()
	if s.emitter != nil {
		s.emitter.flush()
	}
}

// otlpRedactedWriter routes bytes released by the persistent redactor to the
// emitter for the command whose write released them. Its stream mutex is held
// by all callers.
type otlpRedactedWriter struct {
	stream *otlpJobLogStream
}

func (w otlpRedactedWriter) Write(data []byte) (int, error) {
	if w.stream.emitter == nil {
		return len(data), nil
	}
	return w.stream.emitter.Write(data)
}

// otlpLineEmitter line-buffers (already redacted) output and emits each line as
// an OpenTelemetry log record with a native timestamp. Write and flush are
// guarded by mu because control output may be written from multiple goroutines
// (e.g. the cancellation handler emitting a comment).
type otlpLineEmitter struct {
	// An io.Writer does not carry a context, and line/redaction buffering can
	// emit after the call that supplied these bytes. Retain the detached context
	// selected by Wrap so delayed records keep command trace correlation after
	// cancellation. A future record-aware buffer could retain a context with
	// each buffered chunk instead.
	ctx   context.Context
	log   otellog.Logger
	attrs []otellog.KeyValue
	mu    sync.Mutex
	buf   []byte
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
	now := time.Now()

	var record otellog.Record
	record.SetTimestamp(now)
	record.SetObservedTimestamp(now)
	record.SetSeverity(otellog.SeverityInfo)
	record.SetSeverityText("INFO")
	record.SetBody(otellog.StringValue(line))
	record.AddAttributes(e.attrs...)
	e.log.Emit(e.ctx, record)
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

func appendLogAttrs(base []otellog.KeyValue, attrs map[string]string) []otellog.KeyValue {
	if len(attrs) == 0 {
		return base
	}
	out := append([]otellog.KeyValue{}, base...)
	for key, value := range attrs {
		if value != "" {
			out = append(out, otellog.String(key, value))
		}
	}
	return out
}
