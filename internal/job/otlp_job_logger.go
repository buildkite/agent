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

type otlpJobLogger struct {
	log       otellog.Logger
	provider  *sdklog.LoggerProvider
	attrs     []otellog.KeyValue
	redactors *replacer.Mux

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
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)),
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
	// Redact OTLP output using the same secret needles as the job-log
	// redactor, so secrets never reach the OTLP backend. A fresh per-command
	// Replacer feeds the line emitter; it is seeded with the needles known at
	// the time the command starts and kept in sync on every write with the
	// live redactor Mux (see otlpJobLogWriter.Write), so secrets added mid
	// command (e.g. via the Job API) are also redacted here.
	return &otlpJobLogWriter{
		out:      out,
		live:     l.redactors,
		redactor: replacer.New(emitter, l.redactors.Needles(), redact.Redacted),
		emitter:  emitter,
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

func (l *otlpJobLogger) Close() error {
	if l.control != nil {
		l.control.flush()
	}
	if l.provider == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	flushErr := l.provider.ForceFlush(ctx)
	shutdownErr := l.provider.Shutdown(ctx)
	if flushErr != nil {
		return flushErr
	}
	return shutdownErr
}

// otlpJobLogWriter tees process output to the normal (already redacting) job-log
// writer and to a redacting OTLP line emitter, so OTLP records carry the same
// redacted content as the customer-facing job log.
type otlpJobLogWriter struct {
	mu       sync.Mutex
	out      io.Writer
	live     *replacer.Mux
	redactor *replacer.Replacer
	emitter  *otlpLineEmitter
}

func (w *otlpJobLogWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	n, err := w.out.Write(data)
	// Keep the OTLP redactor's needles in sync with the live job redactor
	// before redacting, so secrets added mid-command (e.g. via the Job API)
	// are redacted in OTLP output too. Replacer.Add deduplicates, so re-adding
	// the full needle set each write is safe.
	if w.live != nil {
		w.redactor.Add(w.live.Needles()...)
	}
	// Feed the OTLP copy through the redactor, which streams redacted bytes to
	// the line emitter. Errors here must not affect the primary job log.
	_, _ = w.redactor.Write(data)
	return n, err
}

func (w *otlpJobLogWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	_ = w.redactor.Flush()
	w.emitter.flush()
}

// otlpLineEmitter line-buffers (already redacted) output and emits each line as
// an OpenTelemetry log record with a native timestamp. Write and flush are
// guarded by mu because control output may be written from multiple goroutines
// (e.g. the cancellation handler emitting a comment).
type otlpLineEmitter struct {
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
			e.buf = append(e.buf, data...)
			return origLen, nil
		}

		line := append(e.buf, data[:i]...)
		line = bytes.TrimSuffix(line, []byte{'\r'})
		e.emit(string(line))
		e.buf = e.buf[:0]
		data = data[i+1:]
	}

	return origLen, nil
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
