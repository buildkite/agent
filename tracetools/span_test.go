package tracetools

import (
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/log"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/embedded"
)

// TestOpenTracingSpan is a simple opentracing-compatible span to help test.
type TestOpenTracingSpan struct {
	ctx      opentracing.SpanContext
	finished bool
	fields   []log.Field
	tags     map[string]any
}

func (t *TestOpenTracingSpan) Finish()                                       { t.finished = true }
func (t *TestOpenTracingSpan) FinishWithOptions(_ opentracing.FinishOptions) { t.finished = true }
func (t *TestOpenTracingSpan) Context() opentracing.SpanContext              { return t.ctx }
func (t *TestOpenTracingSpan) SetOperationName(_ string) opentracing.Span    { return t }
func (t *TestOpenTracingSpan) SetTag(k string, v any) opentracing.Span {
	t.tags[k] = v
	return t
}
func (t *TestOpenTracingSpan) LogFields(f ...log.Field)                    { t.fields = append(t.fields, f...) }
func (t *TestOpenTracingSpan) LogKV(_ ...any)                              {}
func (t *TestOpenTracingSpan) SetBaggageItem(_, _ string) opentracing.Span { return t }
func (t *TestOpenTracingSpan) BaggageItem(_ string) string                 { return "" }
func (t *TestOpenTracingSpan) Tracer() opentracing.Tracer                  { return nil }
func (t *TestOpenTracingSpan) LogEvent(_ string)                           {}
func (t *TestOpenTracingSpan) LogEventWithPayload(_ string, _ any)         {}
func (t *TestOpenTracingSpan) Log(_ opentracing.LogData)                   {}

func newTestOpenTracingSpan() *OpenTracingSpan {
	return &OpenTracingSpan{Span: &TestOpenTracingSpan{tags: map[string]any{}}}
}

type TestOtelSpan struct {
	embedded.Span

	finished       bool
	err            error
	events         []string
	spanContext    trace.SpanContext
	statusCode     codes.Code
	statusDesc     string
	name           string
	links          []trace.Link
	attributes     []attribute.KeyValue
	tracerProvider trace.TracerProvider
}

var _ trace.Span = (*TestOtelSpan)(nil)

func (t *TestOtelSpan) End(options ...trace.SpanEndOption)            { t.finished = true }
func (t *TestOtelSpan) IsRecording() bool                             { return !t.finished }
func (t *TestOtelSpan) RecordError(err error, _ ...trace.EventOption) { t.err = err }
func (t *TestOtelSpan) SpanContext() trace.SpanContext                { return t.spanContext }
func (t *TestOtelSpan) SetName(name string)                           { t.name = name }
func (t *TestOtelSpan) TracerProvider() trace.TracerProvider          { return t.tracerProvider }
func (t *TestOtelSpan) AddLink(link trace.Link)                       { t.links = append(t.links, link) }

func (t *TestOtelSpan) SetAttributes(kv ...attribute.KeyValue) {
	t.attributes = append(t.attributes, kv...)
}

func (t *TestOtelSpan) SetStatus(code codes.Code, description string) {
	t.statusCode, t.statusDesc = code, description
}

func (t *TestOtelSpan) AddEvent(name string, _ ...trace.EventOption) {
	t.events = append(t.events, name)
}

func newTestOtelSpan() *OpenTelemetrySpan {
	return &OpenTelemetrySpan{Span: &TestOtelSpan{events: []string{}, attributes: []attribute.KeyValue{}}}
}

func TestAddAttribute_OpenTracing(t *testing.T) {
	t.Parallel()

	span := newTestOpenTracingSpan()
	implSpan, ok := span.Span.(*TestOpenTracingSpan)
	if got := ok; !got {
		t.Errorf("ok = %t, want true", got)
	}

	if got := len(implSpan.tags); got != 0 {
		t.Errorf("len(implSpan.tags) = %v, want 0", got)
	}

	span.AddAttributes(map[string]string{"colour": "green", "flavour": "spicy"})
	if diff := cmp.Diff(implSpan.tags, map[string]any{"colour": "green", "flavour": "spicy"}); diff != "" {
		t.Errorf("implSpan.tags diff (-got +want):\n%s", diff)
	}
}

func TestAddAttributeToSpan_OpenTelemetry(t *testing.T) {
	t.Parallel()

	span := newTestOtelSpan()
	implSpan, ok := span.Span.(*TestOtelSpan)
	if got := ok; !got {
		t.Errorf("ok = %t, want true", got)
	}

	if got := len(implSpan.attributes); got != 0 {
		t.Errorf("len(implSpan.attributes) = %v, want 0", got)
	}

	span.AddAttributes(map[string]string{"colour": "blue", "flavour": "bittersweet"})
	if got, want := implSpan.attributes, attribute.String("colour", "blue"); !slices.Contains(got, want) {
		t.Errorf("implSpan.attributes = %v, want containing %v", got, want)
	}
	if got, want := implSpan.attributes, attribute.String("flavour", "bittersweet"); !slices.Contains(got, want) {
		t.Errorf("implSpan.attributes = %v, want containing %v", got, want)
	}
}

func TestFinishWithError_OpenTracing(t *testing.T) {
	t.Parallel()
	err := errors.New("test error")

	span := newTestOpenTracingSpan()
	implSpan, ok := span.Span.(*TestOpenTracingSpan)
	if got := ok; !got {
		t.Errorf("ok = %t, want true", got)
	}

	span.FinishWithError(err)
	if got := implSpan.finished; !got {
		t.Errorf("implSpan.finished = %t, want true", got)
	}
	if diff := cmp.Diff(implSpan.tags["error"], true); diff != "" {
		t.Errorf("implSpan.tags[\"error\"] diff (-got +want):\n%s", diff)
	}
	// TODO: make log.Fields easier to compare
	if got, want := implSpan.fields, ([]log.Field{log.Event("error"), log.Error(err)}); !reflect.DeepEqual(got, want) {
		t.Errorf("implSpan.fields = %v, want %v", got, want)
	}

	span = newTestOpenTracingSpan()
	implSpan, ok = span.Span.(*TestOpenTracingSpan)
	if got := ok; !got {
		t.Errorf("ok = %t, want true", got)
	}

	span.FinishWithError(nil)
	if got := implSpan.finished; !got {
		t.Errorf("implSpan.finished = %t, want true", got)
	}
	got := implSpan.tags
	want := "error"
	if _, has := got[want]; has {
		t.Errorf("implSpan.tags = %v, want containing %q", got, want)
	}
	if got := len(implSpan.fields); got != 0 {
		t.Errorf("len(implSpan.fields) = %v, want 0", got)
	}
}

func TestFinishWithError_OpenTelemetry(t *testing.T) {
	t.Parallel()
	err := errors.New("test error")

	span := newTestOtelSpan()
	implSpan, ok := span.Span.(*TestOtelSpan)
	if got := ok; !got {
		t.Errorf("ok = %t, want true", got)
	}

	span.FinishWithError(err)
	if got := implSpan.finished; !got {
		t.Errorf("implSpan.finished = %t, want true", got)
	}
	if err, want := implSpan.err, err; !errors.Is(err, want) {
		t.Errorf("implSpan.err error = %v, want %v", err, want)
	}
	if got, want := implSpan.statusCode, codes.Error; got != want {
		t.Errorf("implSpan.statusCode = %d, want %d", got, want)
	}
	if got, want := implSpan.statusDesc, "failed"; got != want {
		t.Errorf("implSpan.statusDesc = %q, want %q", got, want)
	}

	span = newTestOtelSpan()
	implSpan, ok = span.Span.(*TestOtelSpan)
	if got := ok; !got {
		t.Errorf("ok = %t, want true", got)
	}

	span.FinishWithError(nil)
	if got := implSpan.finished; !got {
		t.Errorf("implSpan.finished = %t, want true", got)
	}
	if err := implSpan.err; err != nil {
		t.Errorf("implSpan.err error = %v, want nil", err)
	}
	if got, want := implSpan.statusCode, codes.Unset; got != want {
		t.Errorf("implSpan.statusCode = %d, want %d", got, want)
	}
}
