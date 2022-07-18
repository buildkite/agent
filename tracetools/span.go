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
	BackendDatadog       = "datadog"
	BackendOpenTelemetry = "opentelemetry"
	BackendNone          = ""
)

var ValidTracingBackends = map[string]struct{}{
	BackendDatadog:       {},
	BackendOpenTelemetry: {},
	BackendNone:          {},
}

// StartSpanFromContext will start a span appropriate to the given tracing backend from the given context with the given
// operation name. It will also do some common/repeated setup on the span to keep code a little more DRY.
// If an unknown tracing backend is specified, it will return a span that noops on every operation
func StartSpanFromContext(ctx context.Context, operation string, tracingBackend string) (Span, context.Context) {
	switch tracingBackend {
	case BackendDatadog:
		span, ctx := opentracing.StartSpanFromContext(ctx, operation)
		span.SetTag(ddext.AnalyticsEvent, true) // Make the span available for analytics in Datadog
		return NewOpenTracingSpan(span), ctx

	case BackendOpenTelemetry:
		ctx, span := otel.Tracer("buildkite-agent").Start(ctx, operation)
		span.SetAttributes(attribute.String("analytics.event", "true"))
		return &OpenTelemetrySpan{Span: span}, ctx

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
	Span opentracing.Span
}

func NewOpenTracingSpan(base opentracing.Span) *OpenTracingSpan {
	return &OpenTracingSpan{Span: base}
}

// AddAttributes adds the given map of attributes to the span as OpenTracing tags
func (s *OpenTracingSpan) AddAttributes(attributes map[string]string) {
	for k, v := range attributes {
		s.Span.SetTag(k, v)
	}
}

// FinishWithError adds error information to the OpenTracingSpan if error isn't nil, and records the span as having finished
func (s *OpenTracingSpan) FinishWithError(err error) {
	s.RecordError(err)
	s.Span.Finish()
}

// RecordError records an error on the given span
func (s *OpenTracingSpan) RecordError(err error) {
	if err == nil {
		return
	}

	ext.LogError(s.Span, err)
}

type OpenTelemetrySpan struct {
	Span trace.Span
}

func NewOpenTelemetrySpan(base trace.Span) *OpenTelemetrySpan {
	return &OpenTelemetrySpan{Span: base}
}

// AddAttributes adds the given attributes to the OpenTelemetry span. Only string attributes are accepted.
func (s *OpenTelemetrySpan) AddAttributes(attributes map[string]string) {
	for k, v := range attributes {
		s.Span.SetAttributes(attribute.String(k, v))
	}
}

// FinishWithError adds error information to the OpenTelemetry span if error isn't nil, and records the span as having finished
func (s *OpenTelemetrySpan) FinishWithError(err error) {
	s.RecordError(err)
	s.Span.End()
}

// RecordError records an error on the given OpenTelemetry span. No-op when error is nil
func (s *OpenTelemetrySpan) RecordError(err error) {
	if err == nil {
		return
	}

	s.Span.RecordError(err)
	s.Span.SetStatus(codes.Error, "failed")
}

// NoopSpan is an implementation of the Span interface that does nothing for every method implemented
// The intended use case is for instances where the user doesn't have tracing enabled - using NoopSpan, we can still act
// as though tracing is enabled, but every time we do something tracing related, nothing happens.
type NoopSpan struct{}

// AddAttributes is a noop
func (s *NoopSpan) AddAttributes(attributes map[string]string) {}

// FinishWithError is a noop
func (s *NoopSpan) FinishWithError(err error) {}

// RecordError is a noop
func (s *NoopSpan) RecordError(err error) {}
