package agent

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
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

// OTLPJobLogger satisfies io.WriteCloser and emits job output as OpenTelemetry
// log records. It line-buffers process output so each emitted OTLP record can
// carry a native timestamp instead of encoding timestamps into the log body.
type OTLPJobLogger struct {
	ctx      context.Context
	log      otellog.Logger
	provider *sdklog.LoggerProvider
	attrs    []otellog.KeyValue

	mu                sync.Mutex
	buf               []byte
	currentPhase      string
	currentHook       string
	currentHookScope  string
	currentHookPlugin string
}

func NewOTLPJobLogger(ctx context.Context, conf JobRunnerConfig) (*OTLPJobLogger, error) {
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

	serviceName := conf.AgentConfiguration.TracingServiceName
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
	log := provider.Logger(
		"buildkite-agent",
		otellog.WithInstrumentationVersion(version.Version()),
		otellog.WithSchemaURL(semconv.SchemaURL),
	)

	return newOTLPJobLoggerWithLogger(ctx, log, provider, otlpJobLogAttributes(conf)), nil
}

func newOTLPJobLoggerWithLogger(ctx context.Context, log otellog.Logger, provider *sdklog.LoggerProvider, attrs []otellog.KeyValue) *OTLPJobLogger {
	ctx = context.WithoutCancel(ctx)
	return &OTLPJobLogger{
		ctx:      ctx,
		log:      log,
		provider: provider,
		attrs:    attrs,
	}
}

func (l *OTLPJobLogger) Write(data []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	origLen := len(data)
	for len(data) > 0 {
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			l.buf = append(l.buf, data...)
			return origLen, nil
		}

		line := append(l.buf, data[:i]...)
		line = bytes.TrimSuffix(line, []byte{'\r'})
		l.emit(string(line))
		l.buf = l.buf[:0]
		data = data[i+1:]
	}

	return origLen, nil
}

func (l *OTLPJobLogger) Close() error {
	l.mu.Lock()
	if len(l.buf) > 0 {
		l.emit(string(l.buf))
		l.buf = l.buf[:0]
	}
	l.mu.Unlock()

	if l.provider == nil {
		return nil
	}

	flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	flushErr := l.provider.ForceFlush(flushCtx)
	shutdownErr := l.provider.Shutdown(flushCtx)
	if flushErr != nil {
		return flushErr
	}
	return shutdownErr
}

func (l *OTLPJobLogger) emit(line string) {
	l.updateScope(line)

	now := time.Now()
	attrs := append([]otellog.KeyValue{}, l.attrs...)
	if l.currentPhase != "" {
		attrs = append(attrs, otellog.String("buildkite.phase", l.currentPhase))
	}
	if l.currentHook != "" {
		attrs = append(attrs,
			otellog.String("buildkite.hook.name", l.currentHook),
			otellog.String("buildkite.hook.scope", l.currentHookScope),
		)
		if l.currentHookPlugin != "" {
			attrs = append(attrs, otellog.String("buildkite.hook.plugin", l.currentHookPlugin))
		}
	}

	var record otellog.Record
	record.SetTimestamp(now)
	record.SetObservedTimestamp(now)
	record.SetSeverity(otellog.SeverityInfo)
	record.SetSeverityText("INFO")
	record.SetBody(otellog.StringValue(line))
	record.AddAttributes(attrs...)
	l.log.Emit(l.ctx, record)
}

func (l *OTLPJobLogger) updateScope(line string) {
	header := ansiColourRE.ReplaceAllString(line, "")
	if !headerRE.MatchString(header) {
		return
	}

	parts := strings.Fields(header)
	if len(parts) < 2 {
		return
	}
	rest := strings.TrimSpace(strings.TrimPrefix(header, parts[0]))

	if strings.Contains(rest, "Running commands") || strings.Contains(rest, "Running script") {
		l.currentPhase = "command"
		l.currentHook = "command"
		l.currentHookScope = "default"
		l.currentHookPlugin = ""
		return
	}

	i := strings.Index(rest, "Running ")
	if i < 0 {
		return
	}
	hook := strings.TrimSpace(rest[i+len("Running "):])
	hook = strings.TrimSuffix(hook, "\r")
	hook = strings.TrimSuffix(hook, " hook")
	fields := strings.Fields(hook)
	if len(fields) < 2 || !isKnownHook(fields[len(fields)-1]) {
		return
	}

	l.currentPhase = "hook"
	l.currentHookScope = fields[0]
	l.currentHook = fields[len(fields)-1]
	l.currentHookPlugin = strings.Join(fields[1:len(fields)-1], " ")
}

func isKnownHook(name string) bool {
	switch name {
	case "environment", "pre-checkout", "post-checkout", "pre-command", "command", "post-command", "pre-artifact", "post-artifact", "pre-exit":
		return true
	default:
		return false
	}
}

func otlpJobLogAttributes(conf JobRunnerConfig) []otellog.KeyValue {
	job := conf.Job
	env := job.Env
	attrs := []otellog.KeyValue{
		otellog.String("source", "job"),
		otellog.String("buildkite.organization.slug", env["BUILDKITE_ORGANIZATION_SLUG"]),
		otellog.String("buildkite.pipeline.slug", env["BUILDKITE_PIPELINE_SLUG"]),
		otellog.String("buildkite.branch", env["BUILDKITE_BRANCH"]),
		otellog.String("buildkite.queue", env["BUILDKITE_AGENT_META_DATA_QUEUE"]),
		otellog.String("buildkite.agent", env["BUILDKITE_AGENT_NAME"]),
		otellog.String("buildkite.agent.id", env["BUILDKITE_AGENT_ID"]),
		otellog.String("buildkite.build.id", env["BUILDKITE_BUILD_ID"]),
		otellog.String("buildkite.build.number", env["BUILDKITE_BUILD_NUMBER"]),
		otellog.String("buildkite.job.id", job.ID),
		otellog.String("buildkite.job.label", env["BUILDKITE_LABEL"]),
		otellog.String("buildkite.job.key", env["BUILDKITE_STEP_KEY"]),
	}

	return attrs
}
