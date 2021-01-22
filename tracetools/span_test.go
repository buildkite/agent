package tracetools

import (
	"errors"
	"testing"

	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/log"
	"github.com/stretchr/testify/assert"
)

// TestSpan is a simple opentracing-compatible span to help test.
type TestSpan struct {
	ctx      opentracing.SpanContext
	finished bool
	fields   []log.Field
	err      error
	tags     map[string]interface{}
}

func (t *TestSpan) Finish()                                         { t.finished = true }
func (t *TestSpan) FinishWithOptions(_ opentracing.FinishOptions)   { t.finished = true }
func (t *TestSpan) Context() opentracing.SpanContext                { return t.ctx }
func (t *TestSpan) SetOperationName(_ string) opentracing.Span      { return t }
func (t *TestSpan) SetTag(k string, v interface{}) opentracing.Span { t.tags[k] = v; return t }
func (t *TestSpan) LogFields(f ...log.Field)                        { t.fields = append(t.fields, f...) }
func (t *TestSpan) LogKV(_ ...interface{})                          {}
func (t *TestSpan) SetBaggageItem(_, _ string) opentracing.Span     { return t }
func (t *TestSpan) BaggageItem(_ string) string                     { return "" }
func (t *TestSpan) Tracer() opentracing.Tracer                      { return nil }
func (t *TestSpan) LogEvent(_ string)                               {}
func (t *TestSpan) LogEventWithPayload(_ string, _ interface{})     {}
func (t *TestSpan) Log(_ opentracing.LogData)                       {}

func newSpan() *TestSpan {
	return &TestSpan{tags: map[string]interface{}{}}
}

func TestFinishWithError(t *testing.T) {
	span := newSpan()
	err := errors.New("asd")
	FinishWithError(span, err, log.String("a", "b"), log.Int("c", 1))
	assert.True(t, span.finished)
	assert.Equal(t, true, span.tags["error"])
	assert.Equal(t, []log.Field{log.Event("error"), log.Error(err), log.String("a", "b"), log.Int("c", 1)}, span.fields)

	span = newSpan()
	FinishWithError(span, err)
	assert.True(t, span.finished)
	assert.Equal(t, true, span.tags["error"])
	assert.Equal(t, []log.Field{log.Event("error"), log.Error(err)}, span.fields)

	span = newSpan()
	FinishWithError(span, nil)
	assert.True(t, span.finished)
	assert.NotContains(t, span.tags, "error")
	assert.Empty(t, span.fields)
}
