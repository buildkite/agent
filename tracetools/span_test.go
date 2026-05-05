package tracetools

import (
	"errors"
	"slices"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/embedded"
)

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

func newTestOtelSpan() *TestOtelSpan {
	return &TestOtelSpan{events: []string{}, attributes: []attribute.KeyValue{}}
}

func TestAddAttributes(t *testing.T) {
	t.Parallel()

	span := newTestOtelSpan()

	if got := len(span.attributes); got != 0 {
		t.Errorf("span.attributes = %v, want 0", got)
	}

	AddAttributes(span, map[string]string{"colour": "blue", "flavour": "bittersweet"})
	if got, want := span.attributes, attribute.String("colour", "blue"); !slices.Contains(got, want) {
		t.Errorf("span.attributes = %v, want containing %v", got, want)
	}
	if got, want := span.attributes, attribute.String("flavour", "bittersweet"); !slices.Contains(got, want) {
		t.Errorf("span.attributes = %v, want containing %v", got, want)
	}
}

func TestFinishWithError(t *testing.T) {
	t.Parallel()
	err := errors.New("test error")

	span := newTestOtelSpan()

	FinishWithError(span, err)
	if got := span.finished; !got {
		t.Errorf("span.finished = %t, want true", got)
	}
	if err, want := span.err, err; !errors.Is(err, want) {
		t.Errorf("span.err error = %v, want %v", err, want)
	}
	if got, want := codes.Error, span.statusCode; got != want {
		t.Errorf("codes.Error = %d, want %d", got, want)
	}
	if got, want := span.statusDesc, "failed"; got != want {
		t.Errorf("span.statusDesc = %q, want %q", got, want)
	}

	span = newTestOtelSpan()

	FinishWithError(span, nil)
	if got := span.finished; !got {
		t.Errorf("span.finished = %t, want true", got)
	}
	if err := span.err; err != nil {
		t.Errorf("span.err error = %v, want nil", err)
	}
	if got, want := codes.Unset, span.statusCode; got != want {
		t.Errorf("codes.Unset = %d, want %d", got, want)
	}
}
