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

type otlpJobLogger struct {
	log      otellog.Logger
	provider *sdklog.LoggerProvider
	attrs    []otellog.KeyValue
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
		provider: provider,
		attrs:    otlpJobAttributes(e),
	}, nil
}

func (l *otlpJobLogger) Wrap(ctx context.Context, out io.Writer, attrs map[string]string) io.Writer {
	return &otlpJobLogWriter{
		ctx:   context.WithoutCancel(ctx),
		out:   out,
		log:   l.log,
		attrs: appendLogAttrs(l.attrs, attrs),
	}
}

func (l *otlpJobLogger) Close() error {
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

type otlpJobLogWriter struct {
	mu    sync.Mutex
	ctx   context.Context
	out   io.Writer
	log   otellog.Logger
	attrs []otellog.KeyValue
	buf   []byte
}

func (w *otlpJobLogWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	n, err := w.out.Write(data)
	origLen := len(data)
	for len(data) > 0 {
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			w.buf = append(w.buf, data...)
			return n, err
		}

		line := append(w.buf, data[:i]...)
		line = bytes.TrimSuffix(line, []byte{'\r'})
		w.emit(string(line))
		w.buf = w.buf[:0]
		data = data[i+1:]
	}

	return origLen, err
}

func (w *otlpJobLogWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.buf) == 0 {
		return
	}
	w.emit(string(w.buf))
	w.buf = w.buf[:0]
}

func (w *otlpJobLogWriter) emit(line string) {
	now := time.Now()

	var record otellog.Record
	record.SetTimestamp(now)
	record.SetObservedTimestamp(now)
	record.SetSeverity(otellog.SeverityInfo)
	record.SetSeverityText("INFO")
	record.SetBody(otellog.StringValue(line))
	record.AddAttributes(w.attrs...)
	w.log.Emit(w.ctx, record)
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
