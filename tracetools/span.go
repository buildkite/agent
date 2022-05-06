package tracetools

import (
	"context"

	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	ddext "gopkg.in/DataDog/dd-trace-go.v1/ddtrace/ext"
)

const (
	BackendDatadog                    = "datadog"
	BackendOpenTelemetry_Experimental = "opentelemetry-experimental"
	BackendNone                       = ""
)

// StartSpanFromContext will start a span appropriate to the given tracing backend from the given context with the given
// operation name. It will also do some common/repeated setup on the span to keep code a little more DRY.
// If an unknown tracing backend is specified, it will return a span that noops on every operation
func StartSpanFromContext(ctx context.Context, operation string, tracingBackend string) (Span, context.Context) {
	switch tracingBackend {
	case BackendDatadog:
		span, ctx := opentracing.StartSpanFromContext(ctx, operation)
		span.SetTag(ddext.AnalyticsEvent, true) // Make the span available for analytics in Datadog
		return &OpenTracingSpan{span: span}, ctx

	case BackendOpenTelemetry_Experimental:
		ctx, span := otel.Tracer("buildkite_agent").Start(ctx, operation)
		span.SetAttributes(attribute.String("analytics.event", "true"))
		return &OpenTelemetrySpan{span: span}, ctx

	case BackendNone:
		fallthrough

	default:
		return &NoopSpan{}, ctx
	}
}

type Span interface {
	AddAttributes(map[string]string)
	FinishWithError(error)
	RecordError(error)
}

type OpenTracingSpan struct {
	span opentracing.Span
}

func NewOpenTracingSpan(base opentracing.Span) *OpenTracingSpan {
	return &OpenTracingSpan{span: base}
}

// Adds context to the span, either as opentracing tags, or as opentelemetry attributes
// Will panic if span is anything other than an opentracing.Span, a trace.Span, or nil
// Noop when span is nil
func (s *OpenTracingSpan) AddAttributes(attributes map[string]string) {
	for k, v := range attributes {
		s.span.SetTag(k, v)
	}
}

// FinishWithError adds error information to the OpenTracingSpan if error isn't nil, and records the span as having finished
func (s *OpenTracingSpan) FinishWithError(err error) {
	s.RecordError(err)
	s.span.Finish()
}

// RecordError records an error on the given span.
// Will panic if span is anything other than an opentracing.Span, a trace.Span, or nil
// noop when span or error are nil
func (s *OpenTracingSpan) RecordError(err error) {
	if err == nil {
		return
	}

	ext.LogError(s.span, err)
}

type OpenTelemetrySpan struct {
	span trace.Span
}

func NewOpenTelemetrySpan(base trace.Span) *OpenTelemetrySpan {
	return &OpenTelemetrySpan{span: base}
}

// Adds context to the span, either as opentracing tags, or as opentelemetry attributes
// Will panic if span is anything other than an opentracing.Span, a trace.Span, or nil
// Noop when span is nil
func (s *OpenTelemetrySpan) AddAttributes(attributes map[string]string) {
	for k, v := range attributes {
		s.span.SetAttributes(attribute.String(k, v))
	}
}

// FinishWithError is syntactic sugar for opentracing APIs to add errors to a span
// and then finishing it. If the error is nil, the span will only be finished.
// Will panic if span is anything other than an opentracing.Span, a trace.Span, or nil
// Noop when span is nil
func (s *OpenTelemetrySpan) FinishWithError(err error) {
	s.RecordError(err)
	s.span.End()
}

// RecordError records an error on the given span.
// Will panic if span is anything other than an opentracing.Span, a trace.Span, or nil
// noop when span or error are nil
func (s *OpenTelemetrySpan) RecordError(err error) {
	if err == nil {
		return
	}

	s.span.RecordError(err)
	s.span.SetStatus(codes.Error, "failed")
}

type NoopSpan struct{}

func (s *NoopSpan) AddAttributes(attributes map[string]string) {}
func (s *NoopSpan) FinishWithError(err error)                  {}
func (s *NoopSpan) RecordError(err error)                      {}
