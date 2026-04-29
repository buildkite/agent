package job

import (
	"context"
	"fmt"
	"maps"
	"os"
	"strconv"
	"strings"

	"github.com/buildkite/agent/v4/env"
	"github.com/buildkite/agent/v4/tracetools"
	"github.com/buildkite/agent/v4/version"
	"go.opentelemetry.io/contrib/propagators/aws/xray"
	"go.opentelemetry.io/contrib/propagators/b3"
	"go.opentelemetry.io/contrib/propagators/jaeger"
	"go.opentelemetry.io/contrib/propagators/ot"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	"go.opentelemetry.io/otel/trace"
)

// stopper lets us abstract the tracer wrap up code.
type stopper func()

func noopStopper() {}

func (e *Executor) startTracing(ctx context.Context) (trace.Span, context.Context, stopper) {
	switch e.TracingBackend {
	case tracetools.BackendOpenTelemetry:
		return e.startTracingOpenTelemetry(ctx)

	case tracetools.BackendNone:
		span, ctx := tracetools.StartSpanFromContext(ctx, "noop", tracetools.BackendNone)
		return span, ctx, noopStopper

	default:
		e.shell.Commentf("An invalid tracing backend was provided: %q. Tracing will not occur.", e.TracingBackend)
		e.TracingBackend = tracetools.BackendNone // Ensure that we don't do any tracing after this, some of the stuff in tracetools uses the job's tracking backend
		span, ctx := tracetools.StartSpanFromContext(ctx, "noop", tracetools.BackendNone)
		return span, ctx, noopStopper
	}
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

func (e *Executor) startTracingOpenTelemetry(ctx context.Context) (trace.Span, context.Context, stopper) {
	// Set up trace exporter based on protocol
	protocol := os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL")
	// default to grpc to avoid breaking change
	if protocol == "" {
		protocol = "grpc"
	}

	var exporter sdktrace.SpanExporter
	var err error
	switch protocol {
	case "grpc":
		exporter, err = otlptracegrpc.New(ctx)
	case "http/protobuf", "http":
		exporter, err = otlptracehttp.New(ctx)
	default:
		e.shell.Errorf("Unsupported OTLP protocol: %s. Disabling tracing.", protocol)
		span, ctx := tracetools.StartSpanFromContext(ctx, "noop", tracetools.BackendNone)
		return span, ctx, noopStopper
	}
	if err != nil {
		e.shell.Errorf("Error creating OTLP trace exporter %s. Disabling tracing.", err)
		span, ctx := tracetools.StartSpanFromContext(ctx, "noop", tracetools.BackendNone)
		return span, ctx, noopStopper
	}

	attributes := []attribute.KeyValue{
		semconv.ServiceNameKey.String(e.TracingServiceName),
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

	ctx = e.contextWithTraceparentIfEnabled(ctx)
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

	return span, ctx, stop
}

// accepting traceparent from Buildkite control plane is an opt-in feature as its
// technically a breaking change to the behaviour, and if the server-side tracing
// isn't set up correctly, agent traces may end up without root spans to link to
func (e *Executor) contextWithTraceparentIfEnabled(ctx context.Context) context.Context {
	if !e.TracingPropagateTraceparent {
		return ctx
	}

	if e.TracingTraceParent == "" {
		e.shell.Warningf("tracing-propagate-traceparent enabled, but no traceparent provided by server")
		return ctx
	}

	return otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier{
		"traceparent": e.TracingTraceParent,
	})
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

	result := map[string]any{
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

	// Add agent metadata from BUILDKITE_AGENT_META_DATA_* env vars
	// These come from the agent's registration tags
	const metaDataPrefix = "BUILDKITE_AGENT_META_DATA_"
	for key, value := range env.Dump() {
		if after, found := strings.CutPrefix(key, metaDataPrefix); found {
			// Convert key to lowercase for attribute naming
			attrKey := "buildkite.agent.metadata." + strings.ToLower(after)
			result[attrKey] = value
		}
	}

	return result
}

func Merge(ms ...map[string]any) map[string]any {
	fullCap := 0
	for _, m := range ms {
		fullCap += len(m)
	}

	merged := make(map[string]any, fullCap)
	for _, m := range ms {
		maps.Copy(merged, m)
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
