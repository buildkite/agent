package tracetools

import (
	"errors"
	"testing"

	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/log"
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// TestOpenTracingSpan is a simple opentracing-compatible span to help test.
type TestOpenTracingSpan struct {
	ctx      opentracing.SpanContext
	finished bool
	fields   []log.Field
	err      error
	tags     map[string]interface{}
}

func (t *TestOpenTracingSpan) Finish()                                       { t.finished = true }
func (t *TestOpenTracingSpan) FinishWithOptions(_ opentracing.FinishOptions) { t.finished = true }
func (t *TestOpenTracingSpan) Context() opentracing.SpanContext              { return t.ctx }
func (t *TestOpenTracingSpan) SetOperationName(_ string) opentracing.Span    { return t }
func (t *TestOpenTracingSpan) SetTag(k string, v interface{}) opentracing.Span {
	t.tags[k] = v
	return t
}
func (t *TestOpenTracingSpan) LogFields(f ...log.Field)                    { t.fields = append(t.fields, f...) }
func (t *TestOpenTracingSpan) LogKV(_ ...interface{})                      {}
func (t *TestOpenTracingSpan) SetBaggageItem(_, _ string) opentracing.Span { return t }
func (t *TestOpenTracingSpan) BaggageItem(_ string) string                 { return "" }
func (t *TestOpenTracingSpan) Tracer() opentracing.Tracer                  { return nil }
func (t *TestOpenTracingSpan) LogEvent(_ string)                           {}
func (t *TestOpenTracingSpan) LogEventWithPayload(_ string, _ interface{}) {}
func (t *TestOpenTracingSpan) Log(_ opentracing.LogData)                   {}

func newOpenTracingSpan() *TestOpenTracingSpan {
	return &TestOpenTracingSpan{tags: map[string]interface{}{}}
}

type TestOtelSpan struct {
	finished       bool
	err            error
	events         []string
	spanContext    trace.SpanContext
	statusCode     codes.Code
	statusDesc     string
	name           string
	attributes     []attribute.KeyValue
	tracerProvider trace.TracerProvider
}

func (t *TestOtelSpan) End(options ...trace.SpanEndOption)            { t.finished = true }
func (t *TestOtelSpan) IsRecording() bool                             { return !t.finished }
func (t *TestOtelSpan) RecordError(err error, _ ...trace.EventOption) { t.err = err }
func (t *TestOtelSpan) SpanContext() trace.SpanContext                { return t.spanContext }
func (t *TestOtelSpan) SetName(name string)                           { t.name = name }
func (t *TestOtelSpan) TracerProvider() trace.TracerProvider          { return t.tracerProvider }

func (t *TestOtelSpan) SetAttributes(kv ...attribute.KeyValue) {
	t.attributes = append(t.attributes, kv...)
}

func (t *TestOtelSpan) SetStatus(code codes.Code, description string) {
	t.statusCode, t.statusDesc = code, description
}

func (t *TestOtelSpan) AddEvent(name string, _ ...trace.EventOption) {
	t.events = append(t.events, name)
}

func newOtelSpan() *TestOtelSpan {
	return &TestOtelSpan{events: []string{}, attributes: []attribute.KeyValue{}}
}

func TestAddAttributeToSpan_OpenTracing(t *testing.T) {
	t.Parallel()
	span := newOpenTracingSpan()
	assert.Empty(t, span.tags)

	AddAttributesToSpan(span, map[string]string{"colour": "green", "flavour": "spicy"})
	assert.Equal(t, map[string]interface{}{"colour": "green", "flavour": "spicy"}, span.tags)
}

func TestAddAttributeToSpan_OpenTelemetry(t *testing.T) {
	t.Parallel()
	span := newOtelSpan()
	assert.Empty(t, span.attributes)

	AddAttributesToSpan(span, map[string]string{"colour": "green", "flavour": "spicy"})
	assert.Contains(t, span.attributes, attribute.String("colour", "green"))
	assert.Contains(t, span.attributes, attribute.String("flavour", "spicy"))
}

func TestAddAttributeToSpan_InvalidType(t *testing.T) {
	t.Parallel()
	assert.Panics(t, func() {
		AddAttributesToSpan(12345, map[string]string{"color": "green"})
	})
}

func TestAddAttributeToSpan_Nil(t *testing.T) {
	// AddAttributesToSpan(nil, anything) is a noop, so there's nothing to actually assert, but it is a valid usecase
	// If the tracing backend is "" (ie, tracing is disabled), tracetools.StartSpanFromContext returns a nil span
	// So the tracetools functions that interact with these spans need to be able to handle them
	t.Parallel()
	AddAttributesToSpan(nil, map[string]string{"colour": "green", "flavour": "spicy"})
}

func TestFinishWithError_OpenTracing(t *testing.T) {
	t.Parallel()
	err := errors.New("asd")
	span := newOpenTracingSpan()
	FinishWithError(span, err)
	assert.True(t, span.finished)
	assert.Equal(t, true, span.tags["error"])
	assert.Equal(t, []log.Field{log.Event("error"), log.Error(err)}, span.fields)

	span = newOpenTracingSpan()
	FinishWithError(span, nil)
	assert.True(t, span.finished)
	assert.NotContains(t, span.tags, "error")
	assert.Empty(t, span.fields)
}

func TestFinishWithError_OpenTelemetry(t *testing.T) {
	t.Parallel()
	err := errors.New("test error")
	span := newOtelSpan()
	FinishWithError(span, err)
	assert.True(t, span.finished)
	assert.ErrorIs(t, span.err, err)
	assert.Equal(t, span.statusCode, codes.Error)
	assert.Equal(t, span.statusDesc, "failed")

	span = newOtelSpan()
	FinishWithError(span, nil)
	assert.True(t, span.finished)
	assert.NoError(t, span.err)
	assert.Equal(t, span.statusCode, codes.Unset)
}

func TestFinishWithError_InvalidType(t *testing.T) {
	t.Parallel()
	assert.Panics(t, func() {
		FinishWithError("this is an invalid type to call with FinishWithError", errors.New("test error"))
	})

	assert.Panics(t, func() {
		FinishWithError("this is an invalid type to call with FinishWithError", nil)
	})
}

func TestFinishWithError_Nil(t *testing.T) {
	// FinishWithError(nil, anything) is a noop, so there's nothing to actually assert, but it is a valid usecase
	// If the tracing backend is "" (ie, tracing is disabled), tracetools.StartSpanFromContext returns a nil span
	// So the tracetools functions that interact with these spans need to be able to handle them
	t.Parallel()
	FinishWithError(nil, nil)
	FinishWithError(nil, errors.New("test error"))
}
