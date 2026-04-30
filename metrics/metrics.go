// Package metrics provides a wrapper around OpenTelemetry metrics collection.
//
// It is intended for internal use by buildkite-agent only.
package metrics

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"sync"
	"time"

	"github.com/buildkite/agent/v4/logger"
	"github.com/buildkite/agent/v4/version"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	otelmetric "go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
)

const (
	defaultOTLPProtocol = "grpc"
	defaultServiceName  = "buildkite-agent"
)

type Collector struct {
	config CollectorConfig
	logger logger.Logger

	mu         sync.Mutex
	started    int
	provider   *sdkmetric.MeterProvider
	meter      otelmetric.Meter
	counters   map[string]otelmetric.Int64Counter
	histograms map[string]otelmetric.Float64Histogram
}

type CollectorConfig struct {
	Enabled     bool
	ServiceName string
}

func NewCollector(l logger.Logger, c CollectorConfig) *Collector {
	if c.ServiceName == "" {
		c.ServiceName = defaultServiceName
	}

	return &Collector{
		config:     c,
		logger:     l,
		counters:   make(map[string]otelmetric.Int64Counter),
		histograms: make(map[string]otelmetric.Float64Histogram),
	}
}

func (c *Collector) Start() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.started++
	if c.started > 1 {
		return nil
	}

	if !c.config.Enabled {
		return nil
	}

	protocol := otlpProtocol()
	c.logger.Info("Starting OpenTelemetry metrics collection using OTLP/%s", protocol)

	provider, err := c.newMeterProvider(context.Background(), protocol)
	if err != nil {
		c.started--
		return err
	}

	c.provider = provider
	c.meter = provider.Meter(
		"buildkite-agent",
		otelmetric.WithInstrumentationVersion(version.Version()),
		otelmetric.WithSchemaURL(semconv.SchemaURL),
	)
	c.counters = make(map[string]otelmetric.Int64Counter)
	c.histograms = make(map[string]otelmetric.Float64Histogram)
	return nil
}

func (c *Collector) Stop() error {
	c.mu.Lock()
	if c.started == 0 {
		c.mu.Unlock()
		return nil
	}

	c.started--
	if c.started > 0 {
		c.mu.Unlock()
		return nil
	}

	provider := c.provider
	c.provider = nil
	c.meter = nil
	c.counters = make(map[string]otelmetric.Int64Counter)
	c.histograms = make(map[string]otelmetric.Float64Histogram)
	c.mu.Unlock()

	if provider != nil {
		c.logger.Info("Stopping metrics collection")

		ctx := context.Background()
		flushErr := provider.ForceFlush(ctx)
		shutdownErr := provider.Shutdown(ctx)
		return errors.Join(flushErr, shutdownErr)
	}

	return nil
}

func (c *Collector) newMeterProvider(ctx context.Context, protocol string) (*sdkmetric.MeterProvider, error) {
	var (
		exporter sdkmetric.Exporter
		err      error
	)

	switch protocol {
	case "grpc":
		exporter, err = otlpmetricgrpc.New(ctx)
	case "http/protobuf", "http":
		exporter, err = otlpmetrichttp.New(ctx)
	default:
		return nil, fmt.Errorf("unsupported OTLP protocol %q", protocol)
	}
	if err != nil {
		return nil, err
	}

	resources := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceNameKey.String(c.config.ServiceName),
		semconv.ServiceVersionKey.String(version.Version()),
		semconv.DeploymentEnvironmentKey.String("ci"),
	)

	return sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter)),
		sdkmetric.WithResource(resources),
	), nil
}

func otlpProtocol() string {
	if protocol := os.Getenv("OTEL_EXPORTER_OTLP_METRICS_PROTOCOL"); protocol != "" {
		return protocol
	}
	if protocol := os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL"); protocol != "" {
		return protocol
	}
	return defaultOTLPProtocol
}

func (c *Collector) Scope(tags Tags) *Scope {
	return &Scope{
		Tags: tags,
		c:    c,
	}
}

type Scope struct {
	Tags Tags
	c    *Collector
}

// Timing sends timing information in milliseconds.
func (s *Scope) Timing(name string, value time.Duration, tags ...Tags) {
	histogram, ok := s.c.histogram(name)
	if !ok {
		return
	}

	mergedTags := s.mergeTags(tags...)
	s.c.logger.Debug("Metrics timing %s=%v %v", name, value, mergedTags.StringSlice())
	histogram.Record(context.Background(), float64(value.Milliseconds()), otelmetric.WithAttributes(mergedTags.Attributes()...))
}

// With returns a scope with more tags added
func (s *Scope) With(tags Tags) *Scope {
	return &Scope{
		Tags: s.mergeTags(tags),
		c:    s.c,
	}
}

// Count tracks how many times something happened.
func (s *Scope) Count(name string, value int64, tags ...Tags) {
	counter, ok := s.c.counter(name)
	if !ok {
		return
	}

	mergedTags := s.mergeTags(tags...)
	s.c.logger.Debug("Metrics count %s=%v %v", name, value, mergedTags.StringSlice())
	counter.Add(context.Background(), value, otelmetric.WithAttributes(mergedTags.Attributes()...))
}

func (c *Collector) counter(name string) (otelmetric.Int64Counter, bool) {
	metricName := formatName(name)

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.meter == nil {
		return nil, false
	}
	if counter, ok := c.counters[metricName]; ok {
		return counter, true
	}

	counter, err := c.meter.Int64Counter(metricName)
	if err != nil {
		c.logger.Error("Metrics counter creation failed: %v", err)
		return nil, false
	}

	c.counters[metricName] = counter
	return counter, true
}

func (c *Collector) histogram(name string) (otelmetric.Float64Histogram, bool) {
	metricName := formatName(name)

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.meter == nil {
		return nil, false
	}
	if histogram, ok := c.histograms[metricName]; ok {
		return histogram, true
	}

	histogram, err := c.meter.Float64Histogram(metricName, otelmetric.WithUnit("ms"))
	if err != nil {
		c.logger.Error("Metrics histogram creation failed: %v", err)
		return nil, false
	}

	c.histograms[metricName] = histogram
	return histogram, true
}

func (s *Scope) mergeTags(tagsSlice ...Tags) Tags {
	merged := Tags{}
	for k, v := range s.Tags {
		merged[formatName(k)] = formatName(v)
	}
	for _, tags := range tagsSlice {
		for k, v := range tags {
			merged[formatName(k)] = formatName(v)
		}
	}
	return merged
}

type Tags map[string]string

func (tags Tags) Attributes() []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, len(tags))
	for k, v := range tags {
		if k != "" && v != "" {
			attrs = append(attrs, attribute.String(formatName(k), formatName(v)))
		}
	}
	return attrs
}

func (tags Tags) StringSlice() []string {
	var stringSlice []string
	for k, v := range tags {
		if k != "" && v != "" {
			stringSlice = append(stringSlice, formatName(k)+":"+formatName(v))
		}
	}
	sort.Strings(stringSlice)
	return stringSlice
}

// Keep metric names and tag keys portable across OpenTelemetry exporters.
var nameRegex = regexp.MustCompile(`[^\._a-zA-Z0-9]+`)

func formatName(name string) string {
	return nameRegex.ReplaceAllString(name, "_")
}
