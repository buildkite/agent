package tracetools

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

const (
	BackendOpenTelemetry = "opentelemetry"
	BackendNone          = ""
)

var ValidTracingBackends = map[string]struct{}{
	BackendOpenTelemetry: {},
	BackendNone:          {},
}

// noopTracer is used when tracing is disabled. Spans started from it are
// non-recording and will not export any data.
var noopTracer = noop.NewTracerProvider().Tracer("buildkite-agent")

// StartSpanFromContext starts a span appropriate to the given tracing backend
// from the given context with the given operation name. It also does some
// common/repeated setup on the span to keep code a little more DRY. If an
// unknown tracing backend is specified, it will return a non-recording span.
func StartSpanFromContext(ctx context.Context, operation, tracingBackend string) (trace.Span, context.Context) {
	switch tracingBackend {
	case BackendOpenTelemetry:
		ctx, span := otel.Tracer("buildkite-agent").Start(ctx, operation)
		span.SetAttributes(attribute.String("analytics.event", "true"))
		return span, ctx

	case BackendNone:
		fallthrough

	default:
		ctx, span := noopTracer.Start(ctx, operation)
		return span, ctx
	}
}

// AddAttributes adds the given map of string attributes to the span.
func AddAttributes(span trace.Span, attributes map[string]string) {
	for k, v := range attributes {
		span.SetAttributes(attribute.String(k, v))
	}
}

// FinishWithError records error information on the span (if err isn't nil) and
// ends the span.
func FinishWithError(span trace.Span, err error) {
	RecordError(span, err)
	span.End()
}

// RecordError records an error on the span. No-op when err is nil.
func RecordError(span trace.Span, err error) {
	if err == nil {
		return
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, "failed")
}
