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

func newOpenTracingSpan() *OpenTracingSpan {
	return &OpenTracingSpan{Span: &TestOpenTracingSpan{tags: map[string]interface{}{}}}
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

func newOtelSpan() *OpenTelemetrySpan {
	return &OpenTelemetrySpan{span: &TestOtelSpan{events: []string{}, attributes: []attribute.KeyValue{}}}
}

func TestAddAttribute_OpenTracing(t *testing.T) {
	t.Parallel()

	span := newOpenTracingSpan()
	implSpan, ok := span.Span.(*TestOpenTracingSpan)
	assert.True(t, ok)

	assert.Empty(t, implSpan.tags)

	span.AddAttributes(map[string]string{"colour": "green", "flavour": "spicy"})
	assert.Equal(t, map[string]interface{}{"colour": "green", "flavour": "spicy"}, implSpan.tags)
}

func TestAddAttributeToSpan_OpenTelemetry(t *testing.T) {
	t.Parallel()

	span := newOtelSpan()
	implSpan, ok := span.span.(*TestOtelSpan)
	assert.True(t, ok)

	assert.Empty(t, implSpan.attributes)

	span.AddAttributes(map[string]string{"colour": "blue", "flavour": "bittersweet"})
	assert.Contains(t, implSpan.attributes, attribute.String("colour", "blue"))
	assert.Contains(t, implSpan.attributes, attribute.String("flavour", "bittersweet"))
}

func TestFinishWithError_OpenTracing(t *testing.T) {
	t.Parallel()
	err := errors.New("test error")

	span := newOpenTracingSpan()
	implSpan, ok := span.Span.(*TestOpenTracingSpan)
	assert.True(t, ok)

	span.FinishWithError(err)
	assert.True(t, implSpan.finished)
	assert.Equal(t, true, implSpan.tags["error"])
	assert.Equal(t, []log.Field{log.Event("error"), log.Error(err)}, implSpan.fields)

	span = newOpenTracingSpan()
	implSpan, ok = span.Span.(*TestOpenTracingSpan)
	assert.True(t, ok)

	span.FinishWithError(nil)
	assert.True(t, implSpan.finished)
	assert.NotContains(t, implSpan.tags, "error")
	assert.Empty(t, implSpan.fields)
}

func TestFinishWithError_OpenTelemetry(t *testing.T) {
	t.Parallel()
	err := errors.New("test error")

	span := newOtelSpan()
	implSpan, ok := span.span.(*TestOtelSpan)
	assert.True(t, ok)

	span.FinishWithError(err)
	assert.True(t, implSpan.finished)
	assert.ErrorIs(t, implSpan.err, err)
	assert.Equal(t, implSpan.statusCode, codes.Error)
	assert.Equal(t, implSpan.statusDesc, "failed")

	span = newOtelSpan()
	implSpan, ok = span.span.(*TestOtelSpan)
	assert.True(t, ok)

	span.FinishWithError(nil)
	assert.True(t, implSpan.finished)
	assert.NoError(t, implSpan.err)
	assert.Equal(t, implSpan.statusCode, codes.Unset)
}
