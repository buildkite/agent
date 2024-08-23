package job

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strconv"

	"github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/tracetools"
	"github.com/buildkite/agent/v3/version"
	"github.com/opentracing/opentracing-go"
	"go.opentelemetry.io/contrib/propagators/aws/xray"
	"go.opentelemetry.io/contrib/propagators/b3"
	"go.opentelemetry.io/contrib/propagators/jaeger"
	"go.opentelemetry.io/contrib/propagators/ot"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	"go.opentelemetry.io/otel/trace"
	ddext "gopkg.in/DataDog/dd-trace-go.v1/ddtrace/ext"
	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace/opentracer"
	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace/tracer"
)

// stopper lets us abstract the tracer wrap up code so we can plug in different tracing
// library implementations that are opentracing compatible. Opentracing itself
// doesn't have a Stop function on its Tracer interface.
type stopper func()

func noopStopper() {}

func (e *Executor) startTracing(ctx context.Context) (tracetools.Span, context.Context, stopper) {
	switch e.ExecutorConfig.TracingBackend {
	case tracetools.BackendDatadog:
		// Newer versions of the tracing libs print out diagnostic info which spams the
		// Buildkite agent logs. Disable it by default unless it's been explicitly set.
		if _, has := os.LookupEnv("DD_TRACE_STARTUP_LOGS"); !has {
			os.Setenv("DD_TRACE_STARTUP_LOGS", "false")
		}

		return e.startTracingDatadog(ctx)

	case tracetools.BackendOpenTelemetry:
		return e.startTracingOpenTelemetry(ctx)

	case tracetools.BackendNone:
		return &tracetools.NoopSpan{}, ctx, noopStopper

	default:
		e.shell.Commentf("An invalid tracing backend was provided: %q. Tracing will not occur.", e.ExecutorConfig.TracingBackend)
		e.ExecutorConfig.TracingBackend = tracetools.BackendNone // Ensure that we don't do any tracing after this, some of the stuff in tracetools uses the job's tracking backend
		return &tracetools.NoopSpan{}, ctx, noopStopper
	}
}

func (e *Executor) ddResourceName() string {
	label, ok := e.shell.Env.Get("BUILDKITE_LABEL")
	if !ok {
		label = "job"
	}

	return e.OrganizationSlug + "/" + e.PipelineSlug + "/" + label
}

// startTracingDatadog sets up tracing based on the config values. It uses opentracing as an
// abstraction so the agent can support multiple libraries if needbe.
func (e *Executor) startTracingDatadog(ctx context.Context) (tracetools.Span, context.Context, stopper) {
	opts := []tracer.StartOption{
		tracer.WithService(e.ExecutorConfig.TracingServiceName),
		tracer.WithSampler(tracer.NewAllSampler()),
		tracer.WithAnalytics(true),
	}

	tags := Merge(GenericTracingExtras(e, e.shell.Env), DDTracingExtras())
	opts = slices.Grow(opts, len(tags))
	for k, v := range tags {
		opts = append(opts, tracer.WithGlobalTag(k, v))
	}

	opentracing.SetGlobalTracer(opentracer.New(opts...))

	wireContext := e.extractDDTraceCtx()

	span := opentracing.StartSpan("job.run",
		opentracing.ChildOf(wireContext),
		opentracing.Tag{Key: ddext.ResourceName, Value: e.ddResourceName()},
	)
	ctx = opentracing.ContextWithSpan(ctx, span)

	return tracetools.NewOpenTracingSpan(span), ctx, tracer.Stop
}

// extractTraceCtx pulls encoded distributed tracing information from the env vars.
// Note: This should match the injectTraceCtx code in shell.
func (e *Executor) extractDDTraceCtx() opentracing.SpanContext {
	sctx, err := tracetools.DecodeTraceContext(e.shell.Env.Dump(), e.ExecutorConfig.TraceContextCodec)
	if err != nil {
		// Return nil so a new span will be created
		return nil
	}
	return sctx
}

func (e *Executor) otRootSpanName() string {
	base := e.OrganizationSlug + "/" + e.PipelineSlug + "/"
	key, ok := e.shell.Env.Get("BUILDKITE_STEP_KEY")
	if ok && key != "" {
		return base + key
	}

	label, ok := e.shell.Env.Get("BUILDKITE_LABEL")
	if ok && label != "" {
		return base + label
	}

	return base + "job"
}

func (e *Executor) startTracingOpenTelemetry(ctx context.Context) (tracetools.Span, context.Context, stopper) {
	client := otlptracegrpc.NewClient()
	exporter, err := otlptrace.New(ctx, client)
	if err != nil {
		e.shell.Errorf("Error creating OTLP trace exporter %s. Disabling tracing.", err)
		return &tracetools.NoopSpan{}, ctx, noopStopper
	}

	attributes := []attribute.KeyValue{
		semconv.ServiceNameKey.String(e.ExecutorConfig.TracingServiceName),
		semconv.ServiceVersionKey.String(version.Version()),
		semconv.DeploymentEnvironmentKey.String("ci"),
	}

	extras, warnings := toOpenTelemetryAttributes(GenericTracingExtras(e, e.shell.Env))
	for k, v := range warnings {
		e.shell.Warningf("Unknown attribute type (key: %v, value: %v (%T)) passed when initialising OpenTelemetry. This is a bug, submit this error message at https://github.com/buildkite/agent/issues", k, v, v)
		e.shell.Warningf("OpenTelemetry will still work, but the attribute %v and its value above will not be included", v)
	}

	attributes = append(attributes, extras...)

	resources := resource.NewWithAttributes(semconv.SchemaURL, attributes...)
	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(resources),
	)

	otel.SetTracerProvider(tracerProvider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
		b3.New(),
		&jaeger.Jaeger{},
		&ot.OT{},
		&xray.Propagator{},
	))

	tracer := tracerProvider.Tracer(
		"buildkite-agent",
		trace.WithInstrumentationVersion(version.Version()),
		trace.WithSchemaURL(semconv.SchemaURL),
	)

	ctx, span := tracer.Start(ctx, e.otRootSpanName(),
		trace.WithAttributes(
			attribute.String("analytics.event", "true"),
		),
	)

	stop := func() {
		ctx := context.Background()
		_ = tracerProvider.ForceFlush(ctx)
		_ = tracerProvider.Shutdown(ctx)
	}

	return tracetools.NewOpenTelemetrySpan(span), ctx, stop
}

func GenericTracingExtras(e *Executor, env *env.Environment) map[string]any {
	buildID, _ := env.Get("BUILDKITE_BUILD_ID")
	buildNumber, _ := env.Get("BUILDKITE_BUILD_NUMBER")
	buildURL, _ := env.Get("BUILDKITE_BUILD_URL")
	jobURL := fmt.Sprintf("%s#%s", buildURL, e.JobID)
	source, _ := env.Get("BUILDKITE_SOURCE")

	retry := 0
	if attemptStr, has := env.Get("BUILDKITE_RETRY_COUNT"); has {
		if parsedRetry, err := strconv.Atoi(attemptStr); err == nil {
			retry = parsedRetry
		}
	}

	parallel := 0
	if parallelStr, has := env.Get("BUILDKITE_PARALLEL_JOB"); has {
		if parsedParallel, err := strconv.Atoi(parallelStr); err == nil {
			parallel = parsedParallel
		}
	}

	rebuiltFromID, has := env.Get("BUILDKITE_REBUILT_FROM_BUILD_NUMBER")
	if !has || rebuiltFromID == "" {
		rebuiltFromID = "n/a"
	}

	triggeredFromID, has := env.Get("BUILDKITE_TRIGGERED_FROM_BUILD_ID")
	if !has || triggeredFromID == "" {
		triggeredFromID = "n/a"
	}

	jobLabel, has := env.Get("BUILDKITE_LABEL")
	if !has || jobLabel == "" {
		jobLabel = "n/a"
	}

	jobKey, has := env.Get("BUILDKITE_STEP_KEY")
	if !has || jobKey == "" {
		jobKey = "n/a"
	}

	return map[string]any{
		"buildkite.agent":             e.AgentName,
		"buildkite.version":           version.Version(),
		"buildkite.queue":             e.Queue,
		"buildkite.org":               e.OrganizationSlug,
		"buildkite.pipeline":          e.PipelineSlug,
		"buildkite.branch":            e.Branch,
		"buildkite.job_label":         jobLabel,
		"buildkite.job_key":           jobKey,
		"buildkite.job_id":            e.JobID,
		"buildkite.job_url":           jobURL,
		"buildkite.build_id":          buildID,
		"buildkite.build_number":      buildNumber,
		"buildkite.build_url":         buildURL,
		"buildkite.source":            source,
		"buildkite.retry":             retry,
		"buildkite.parallel":          parallel,
		"buildkite.rebuilt_from_id":   rebuiltFromID,
		"buildkite.triggered_from_id": triggeredFromID,
	}
}

func DDTracingExtras() map[string]any {
	return map[string]any{
		ddext.AnalyticsEvent:   true,
		ddext.SamplingPriority: ddext.PriorityUserKeep,
	}
}

func Merge(maps ...map[string]any) map[string]any {
	fullCap := 0
	for _, m := range maps {
		fullCap += len(m)
	}

	merged := make(map[string]any, fullCap)
	for _, m := range maps {
		for key, val := range m {
			merged[key] = val
		}
	}

	return merged
}

func toOpenTelemetryAttributes(extras map[string]any) ([]attribute.KeyValue, map[string]any) {
	attrs := make([]attribute.KeyValue, 0, len(extras))
	unknownAttrTypes := make(map[string]any, len(extras))
	for k, v := range extras {
		switch v := v.(type) {
		case string:
			attrs = append(attrs, attribute.String(k, v))
		case int:
			attrs = append(attrs, attribute.Int(k, v))
		case bool:
			attrs = append(attrs, attribute.Bool(k, v))
		default:
			unknownAttrTypes[k] = v
		}
	}

	return attrs, unknownAttrTypes
}

func (e *Executor) implementationSpecificSpanName(otelName, ddName string) string {
	switch e.TracingBackend {
	case tracetools.BackendDatadog:
		return ddName
	case tracetools.BackendOpenTelemetry:
		fallthrough
	default:
		return otelName
	}
}
