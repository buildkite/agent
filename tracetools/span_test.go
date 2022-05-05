package tracetools

import (
	"errors"
	"testing"

	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/log"
	"github.com/stretchr/testify/assert"
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

func TestAddAttribute_OpenTracing(t *testing.T) {
	t.Parallel()

	span := newOpenTracingSpan()
	implSpan, ok := span.Span.(*TestOpenTracingSpan)
	assert.True(t, ok)

	assert.Empty(t, implSpan.tags)

	span.AddAttributes(map[string]string{"colour": "green", "flavour": "spicy"})
	assert.Equal(t, map[string]interface{}{"colour": "green", "flavour": "spicy"}, implSpan.tags)
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
