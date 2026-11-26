package job

import (
	"bytes"
	"context"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/url"
	"time"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/internal/agenthttp"
	"github.com/buildkite/agent/v4/version"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

// InternalTracingExporterEnv carries registration credentials to bootstrap.
// Bootstrap consumes and unsets it before loading the job environment.
const InternalTracingExporterEnv = "BUILDKITE_INTERNAL_TRACING_EXPORTER"

// internalTraceExporter constructs OTLP from an allowlist rather than copying
// customer telemetry and trying to redact it. It deliberately does not use
// OTEL_* configuration, including customer TLS credentials and proxy settings.
type internalTraceExporter struct {
	endpoint   string
	headers    map[string]string
	httpClient *http.Client
}

func newInternalTraceExporter(config *api.TracingExporter) (*internalTraceExporter, error) {
	u, err := url.Parse(config.Endpoint)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.Fragment != "" || config.Protocol != "http/protobuf" {
		return nil, errors.New("invalid internal tracing exporter configuration")
	}
	httpClient := agenthttp.NewClient()
	transport := httpClient.Transport.(*http.Transport).Clone()
	transport.Proxy = nil
	httpClient.Transport = transport
	// Collector credentials must not follow redirects to another destination.
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &internalTraceExporter{
		endpoint: config.Endpoint, headers: maps.Clone(config.Headers), httpClient: httpClient,
	}, nil
}

func (e *internalTraceExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	// Internal telemetry must leave time for the job's other shutdown work.
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	internal := make([]*tracepb.Span, 0, len(spans))
	for _, span := range spans {
		sc, parent := span.SpanContext(), span.Parent()
		traceID, spanID, parentID := sc.TraceID(), sc.SpanID(), parent.SpanID()
		var parentBytes []byte
		if parentID.IsValid() {
			parentBytes = parentID[:]
		}
		status := tracepb.Status_STATUS_CODE_UNSET
		switch span.Status().Code {
		case codes.Ok:
			status = tracepb.Status_STATUS_CODE_OK
		case codes.Error:
			status = tracepb.Status_STATUS_CODE_ERROR
		}
		internal = append(internal, &tracepb.Span{
			TraceId: traceID[:], SpanId: spanID[:], ParentSpanId: parentBytes,
			Name: internalSpanName(span.Name()), Kind: tracepb.Span_SpanKind(span.SpanKind()),
			StartTimeUnixNano: uint64(max(0, span.StartTime().UnixNano())),
			EndTimeUnixNano:   uint64(max(0, span.EndTime().UnixNano())),
			Status:            &tracepb.Status{Code: status},
			Flags:             uint32(sc.TraceFlags()),
		})
	}
	// No customer resource, attributes, events (including OSC messages), links,
	// tracestate, scope names, or status descriptions cross this boundary.
	body, err := proto.Marshal(&collector.ExportTraceServiceRequest{
		ResourceSpans: []*tracepb.ResourceSpans{{
			Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{
				{Key: "service.name", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "buildkite-agent"}}},
				{Key: "service.version", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: version.Version()}}},
			}},
			ScopeSpans: []*tracepb.ScopeSpans{{Spans: internal}},
		}},
	})
	// Internal delivery is best-effort. Do not send collector response bodies
	// or credential-bearing HTTP errors to OTel's global logger.
	if err != nil {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil
	}
	for name, value := range e.headers {
		req.Header.Set(name, value)
	}
	req.Header.Set("Content-Type", "application/x-protobuf")
	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	return nil
}

func (e *internalTraceExporter) Shutdown(context.Context) error {
	e.httpClient.CloseIdleConnections()
	return nil
}

func internalSpanName(name string) string {
	switch name {
	case "environment", "checkout", "repo-checkout", "origin-clonekit", "pre-command", "command", "post-command", "pre-exit",
		"artifacts", "pre-artifact", "artifact-upload", "post-artifact", "default command hook",
		"git.mirror.update", "git.clean.pre", "git.lfs.install", "git.verify_commit", "git.sparse_checkout", "git.checkout",
		"git.lfs.fetch", "git.clean.post", "git.submodules", "git.submodule.update", "git.fetch", "git.clone", "git.dissociate",
		"git.mirror.lock_wait.clone", "git.mirror.clone", "git.mirror.lock_wait.update", "git.mirror.fetch", "git.mirror.snapshot", "git.mirror.lfs.fetch":
		return name
	default:
		return "job.operation"
	}
}
