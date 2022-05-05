package tracetools

import (
	"context"
	"fmt"

	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	ddext "gopkg.in/DataDog/dd-trace-go.v1/ddtrace/ext"
)

// StartSpanFromContext will start a span appropriate to the given tracing backend from the given context with the given
// operation name. It will also do some common/repeated setup on the span to keep code a little more DRY.
// Will panic if tracerType is anything other than "datadog", "otel", or ""
func StartSpanFromContext(ctx context.Context, operation string, tracerType string) (any, context.Context) {
	switch tracerType {
	case "datadog":
		span, ctx := opentracing.StartSpanFromContext(ctx, operation)
		span.SetTag(ddext.AnalyticsEvent, true) // Make the span available for analytics in Datadog
		return span, ctx

	case "otel":
		ctx, span := otel.Tracer("buildkite_agent").Start(ctx, operation)
		span.SetAttributes(attribute.String("analytics.event", "true"))
		return span, ctx

	case "":
		return nil, ctx

	default:
		panic(fmt.Sprintf("Invalid tracing backend %q passed to tracetools.StartSpanFromContext", tracerType))
	}
}

// Adds context to the span, either as opentracing tags, or as opentelemetry attributes
// Will panic if span is anything other than an opentracing.Span, a trace.Span, or nil
// Noop when span is nil
func AddAttributesToSpan(span any, attributes map[string]string) {
	switch span := span.(type) {
	case opentracing.Span:
		for k, v := range attributes {
			span.SetTag(k, v)
		}

	case trace.Span: // OpenTelemetry
		for k, v := range attributes {
			span.SetAttributes(attribute.String(k, v))
		}

	case nil:
		return

	default:
		panic(fmt.Sprintf("Invalid span type: %T passed to tracetools.AddAttributesToSpan", span))
	}
}

// FinishWithError is syntactic sugar for opentracing APIs to add errors to a span
// and then finishing it. If the error is nil, the span will only be finished.
// Will panic if span is anything other than an opentracing.Span, a trace.Span, or nil
// Noop when span is nil
func FinishWithError(span any, err error) {
	RecordError(span, err)

	switch span := span.(type) {
	case opentracing.Span:
		span.Finish()

	case trace.Span: // OpenTelemetry
		span.End()

	case nil:
		return

	default:
		panic(fmt.Sprintf("Invalid span type: %T passed to tracetools.FinishWithError", span))
	}
}

// RecordError records an error on the given span.
// Will panic if span is anything other than an opentracing.Span, a trace.Span, or nil
// noop when span or error are nil
func RecordError(span any, err error) {
	if err == nil {
		return
	}

	switch span := span.(type) {
	case opentracing.Span:
		ext.LogError(span, err)

	case trace.Span: // OpenTelemetry
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed")

	case nil:
		return

	default:
		panic(fmt.Sprintf("Invalid span type: %T passed to tracetools.RecordError", span))
	}
}
