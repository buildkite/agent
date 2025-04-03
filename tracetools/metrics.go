package tracetools

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	// semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
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
		// semconv.ServiceNameKey.String(s.Resource().Attributes()[semconv.ServiceNameKey].AsString()),
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

// func main() {
// 	// Initialize resource describing the service
// 	res, err := resource.New(context.Background(),
// 		resource.WithAttributes(
// 			semconv.ServiceNameKey.String("my-service"),
// 			semconv.ServiceVersionKey.String("1.0.0"),
// 		),
// 	)
// 	if err != nil {
// 		log.Fatalf("Failed to create resource: %v", err)
// 	}

// 	// Set up the prometheus exporter
// 	promExporter, err := prometheus.New()
// 	if err != nil {
// 		log.Fatalf("Failed to create Prometheus exporter: %v", err)
// 	}

// 	// Create a metrics provider
// 	meterProvider := sdkmetric.NewMeterProvider(
// 		sdkmetric.WithReader(promExporter),
// 		sdkmetric.WithResource(res),
// 	)
// 	otel.SetMeterProvider(meterProvider)

// 	// Create a batch span processor for normal span processing
// 	batchProcessor := sdktrace.NewBatchSpanProcessor(
// 		// You would configure an actual exporter here if needed
// 		sdktrace.NewNoopSpanExporter(),
// 	)

// 	// Create our custom SpanMetrics processor
// 	spanMetricsProcessor, err := NewSpanMetricsProcessor(meterProvider, batchProcessor)
// 	if err != nil {
// 		log.Fatalf("Failed to create SpanMetrics processor: %v", err)
// 	}

// 	// Create a tracer provider with our custom processor
// 	tracerProvider := sdktrace.NewTracerProvider(
// 		sdktrace.WithSampler(sdktrace.AlwaysSample()),
// 		sdktrace.WithSpanProcessor(spanMetricsProcessor),
// 		sdktrace.WithResource(res),
// 	)
// 	otel.SetTracerProvider(tracerProvider)

// 	// Expose Prometheus metrics endpoint
// 	http.Handle("/metrics", promhttp.Handler())
// 	go func() {
// 		log.Println("Starting metrics server at :8889")
// 		if err := http.ListenAndServe(":8889", nil); err != nil {
// 			log.Fatalf("Failed to start metrics server: %v", err)
// 		}
// 	}()

// 	// Create a tracer
// 	tracer := tracerProvider.Tracer("my-service-tracer")

// 	// Your application logic here
// 	for {
// 		runSample(tracer)
// 		time.Sleep(1 * time.Second)
// 	}
// }
