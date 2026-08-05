package trace

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
	"go.opentelemetry.io/otel/trace"
)

var tracerName = "github.com/buildkite/agent/v4/internal/cache"

func NewProvider(ctx context.Context, exporter, name, version string) (*sdktrace.TracerProvider, error) {
	res, err := newResource(ctx, name, version)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	var exp sdktrace.SpanExporter
	switch exporter {
	case "grpc":
		exp, err = otlptracegrpc.New(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to create exporter: %w", err)
		}
	default:
		// a null exporter is used for testing
		exp = tracetest.NewNoopExporter()
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
	)

	otel.SetTracerProvider(tp)

	otel.SetTextMapPropagator(
		propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		),
	)

	tracerName = name

	return tp, nil
}

func Start(ctx context.Context, name string) (context.Context, trace.Span) {
	return otel.GetTracerProvider().Tracer(tracerName).Start(ctx, name)
}

func newResource(cxt context.Context, name, version string) (*resource.Resource, error) {
	options := []resource.Option{
		resource.WithSchemaURL(semconv.SchemaURL),
	}
	options = append(options, resource.WithHost())
	options = append(options, resource.WithFromEnv())
	options = append(options, resource.WithAttributes(
		semconv.TelemetrySDKNameKey.String("otelconfig"),
		semconv.TelemetrySDKLanguageGo,
		semconv.TelemetrySDKVersionKey.String(version),
	))

	return resource.New(
		cxt,
		options...,
	)
}

func NewError(span trace.Span, msg string, args ...any) error {
	if span == nil {
		return fmt.Errorf("span is nil: %w", fmt.Errorf(msg, args...))
	}

	span.RecordError(fmt.Errorf(msg, args...))
	span.SetStatus(codes.Error, msg)

	return fmt.Errorf(msg, args...)
}
