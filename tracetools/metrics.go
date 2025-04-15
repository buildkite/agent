package tracetools

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// SpanMetricsProcessor implements a trace processor that generates metrics from spans
type SpanMetricsProcessor struct {
	meter           metric.Meter
	histogramMetric metric.Float64Histogram
	counterMetric   metric.Int64Counter
	errorCounter    metric.Int64Counter
	mutex           sync.Mutex
	nextProcessor   sdktrace.SpanProcessor
}

// NewSpanMetricsProcessor creates a new SpanMetricsProcessor
func NewSpanMetricsProcessor(mp metric.MeterProvider, nextProcessor sdktrace.SpanProcessor) (*SpanMetricsProcessor, error) {
	meter := mp.Meter("span-metrics")

	// Create a histogram for span durations
	histogram, err := meter.Float64Histogram(
		"span.duration",
		metric.WithDescription("The duration of spans"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create duration histogram: %w", err)
	}

	// Create a counter for span counts
	counter, err := meter.Int64Counter(
		"span.count",
		metric.WithDescription("The number of spans processed"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create span counter: %w", err)
	}

	// Create a counter for span errors
	errorCounter, err := meter.Int64Counter(
		"span.errors",
		metric.WithDescription("The number of errored spans"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create error counter: %w", err)
	}

	return &SpanMetricsProcessor{
		meter:           meter,
		histogramMetric: histogram,
		counterMetric:   counter,
		errorCounter:    errorCounter,
		nextProcessor:   nextProcessor,
	}, nil
}

// OnStart implements the SpanProcessor interface
func (smp *SpanMetricsProcessor) OnStart(parent context.Context, s sdktrace.ReadWriteSpan) {
	if smp.nextProcessor != nil {
		smp.nextProcessor.OnStart(parent, s)
	}
}

// OnEnd implements the SpanProcessor interface
func (smp *SpanMetricsProcessor) OnEnd(s sdktrace.ReadOnlySpan) {
	smp.mutex.Lock()
	defer smp.mutex.Unlock()

	// Extract relevant attributes from the span
	attrs := []attribute.KeyValue{
		attribute.String("span.name", s.Name()),
		attribute.String("span.kind", s.SpanKind().String()),
	}

	// Record duration in milliseconds
	durationMs := float64(s.EndTime().Sub(s.StartTime())) / float64(time.Millisecond)
	smp.histogramMetric.Record(context.Background(), durationMs, metric.WithAttributes(attrs...))

	// Record span count
	smp.counterMetric.Add(context.Background(), 1, metric.WithAttributes(attrs...))

	// Record error count if span has error status
	if s.Status().Code == 2 { // Error status
		smp.errorCounter.Add(context.Background(), 1, metric.WithAttributes(attrs...))
	}

	// Pass to next processor if there is one
	if smp.nextProcessor != nil {
		smp.nextProcessor.OnEnd(s)
	}
}

// Shutdown implements the SpanProcessor interface
func (smp *SpanMetricsProcessor) Shutdown(ctx context.Context) error {
	if smp.nextProcessor != nil {
		return smp.nextProcessor.Shutdown(ctx)
	}
	return nil
}

// ForceFlush implements the SpanProcessor interface
func (smp *SpanMetricsProcessor) ForceFlush(ctx context.Context) error {
	if smp.nextProcessor != nil {
		return smp.nextProcessor.ForceFlush(ctx)
	}
	return nil
}
