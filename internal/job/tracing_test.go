package job

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/env"
	"github.com/google/go-cmp/cmp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

func TestOTelIndependentExporters(t *testing.T) {
	previousProvider, previousPropagator := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	t.Cleanup(func() {
		otel.SetTracerProvider(previousProvider)
		otel.SetTextMapPropagator(previousPropagator)
	})
	for _, invalid := range []string{"neither", "customer", "internal", "slow customer"} {
		t.Run(invalid, func(t *testing.T) {
			newSink := func(auth string) (*httptest.Server, <-chan *collector.ExportTraceServiceRequest) {
				requests := make(chan *collector.ExportTraceServiceRequest, 1)
				s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Header.Get("Authorization") != auth || r.URL.Path != "/v1/traces" {
						t.Errorf("wrong destination or credential: %s %q", r.URL, r.Header.Get("Authorization"))
					}
					data, err := io.ReadAll(r.Body)
					if err != nil {
						t.Error(err)
						return
					}
					var request collector.ExportTraceServiceRequest
					if err := proto.Unmarshal(data, &request); err != nil {
						t.Error(err)
						return
					}
					requests <- &request
					if invalid == "slow customer" && auth == "Bearer customer-secret" {
						<-r.Context().Done()
						return
					}
					w.Header().Set("Content-Type", "application/x-protobuf")
				}))
				t.Cleanup(s.Close)
				return s, requests
			}
			customer, customerRequests := newSink("Bearer customer-secret")
			internal, internalRequests := newSink("Bearer internal-secret")
			t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", customer.URL+"/v1/traces")
			t.Setenv("OTEL_EXPORTER_OTLP_TRACES_HEADERS", "Authorization=Bearer%20customer-secret")
			t.Setenv("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL", "http/protobuf")
			exporter := &api.TracingExporter{Endpoint: internal.URL + "/v1/traces", Protocol: "http/protobuf", Headers: map[string]string{"Authorization": "Bearer internal-secret"}}
			if invalid == "customer" {
				t.Setenv("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL", "invalid")
			}
			if invalid == "internal" {
				exporter.Protocol = "invalid"
			}
			provider, err := InitOTelTracerProvider(t.Context(), "private-service", []attribute.KeyValue{attribute.String("private-resource", "private-value")}, true, exporter)
			if (err != nil) != (invalid == "customer" || invalid == "internal") || provider == nil {
				t.Fatalf("InitOTelTracerProvider() = %v, %v", provider, err)
			}
			defer provider.Shutdown(t.Context()) //nolint:errcheck // ForceFlush below checks delivery.
			tracer := provider.Tracer("private-scope", trace.WithSchemaURL("private-schema"), trace.WithInstrumentationAttributes(attribute.String("private-scope-attribute", "private-value")))
			ctx, root := tracer.Start(t.Context(), "private-name", trace.WithAttributes(attribute.String("private-attribute", "private-value")), trace.WithLinks(trace.Link{
				SpanContext: trace.NewSpanContext(trace.SpanContextConfig{
					TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2},
				}),
				Attributes: []attribute.KeyValue{attribute.String("private-link", "private-value")},
			}))
			_, phase := tracer.Start(ctx, "checkout")
			phase.End()
			root.AddEvent("private-event", trace.WithAttributes(attribute.String("private-message", "private-value")))
			root.RecordError(errors.New("private-error"))
			root.SetStatus(codes.Error, "private-status")
			root.End()
			flushBudget := 5 * time.Second
			if invalid == "slow customer" {
				flushBudget = 500 * time.Millisecond
			}
			flushCtx, cancel := context.WithTimeout(t.Context(), flushBudget)
			defer cancel()
			flushErr := provider.ForceFlush(flushCtx)
			if invalid == "slow customer" {
				if !errors.Is(flushErr, context.DeadlineExceeded) {
					t.Fatalf("stalled customer flush error = %v, want deadline exceeded", flushErr)
				}
				select {
				case <-internalRequests:
				default:
					t.Fatal("stalled customer exporter prevented internal delivery")
				}
				return
			}
			if flushErr != nil {
				t.Fatal(flushErr)
			}
			for _, destination := range []struct {
				name     string
				requests <-chan *collector.ExportTraceServiceRequest
			}{{"customer", customerRequests}, {"internal", internalRequests}} {
				if destination.name == invalid {
					select {
					case <-destination.requests:
						t.Errorf("invalid %s exporter received traces", invalid)
					default:
					}
					continue
				}
				var request *collector.ExportTraceServiceRequest
				select {
				case request = <-destination.requests:
				case <-flushCtx.Done():
					t.Fatalf("no delivery to %s", destination.name)
				}
				body, err := proto.Marshal(request)
				if err != nil {
					t.Fatal(err)
				}
				if destination.name == "customer" {
					for _, want := range []string{"private-service", "private-resource", "private-name", "private-scope", "private-schema", "private-attribute", "private-event", "private-error", "private-status", "private-link"} {
						if !bytes.Contains(body, []byte(want)) {
							t.Errorf("customer export lost %q", want)
						}
					}
					continue
				}
				if bytes.Contains(body, []byte("private-")) {
					t.Errorf("internal export contains customer text: %v", request)
				}
				spans := request.ResourceSpans[0].ScopeSpans[0].Spans
				if len(spans) != 2 || spans[0].Name != "checkout" || spans[1].Name != "job.operation" || len(spans[1].ParentSpanId) != 0 || spans[1].Status.GetCode() != tracepb.Status_STATUS_CODE_ERROR {
					t.Errorf("internal export lost phase, parent or status semantics: %v", spans)
				}
			}
		})
	}
}

func TestInternalTracingExportDeadline(t *testing.T) {
	previousProvider, previousPropagator := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	t.Cleanup(func() {
		otel.SetTracerProvider(previousProvider)
		otel.SetTextMapPropagator(previousPropagator)
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		<-r.Context().Done()
	}))
	defer server.Close()
	provider, err := InitOTelTracerProvider(t.Context(), "", nil, false, &api.TracingExporter{
		Endpoint: server.URL + "/v1/traces", Protocol: "http/protobuf",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer provider.Shutdown(t.Context()) //nolint:errcheck // ForceFlush below checks the export deadline.
	_, span := provider.Tracer("test").Start(t.Context(), "command")
	span.End()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	if err := provider.ForceFlush(ctx); err != nil {
		t.Fatalf("internal collector exhausted the flush budget: %v", err)
	}
}

func TestOTLPTracesProtocol(t *testing.T) {
	// No t.Parallel: t.Setenv manipulates the process environment. Empty
	// values behave the same as unset ones.
	tests := []struct {
		name, traces, generic, want string
	}{
		{"traces-specific takes precedence over generic", "http/protobuf", "grpc", "http/protobuf"},
		{"generic fallback", "", "http", "http"},
		{"default grpc", "", "", "grpc"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL", tc.traces)
			t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", tc.generic)
			if got := otlpTracesProtocol(); got != tc.want {
				t.Errorf("otlpTracesProtocol() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestOTelResourceAttributesFromEnv(t *testing.T) {
	t.Parallel()

	environ := env.FromMap(map[string]string{
		"BUILDKITE_AGENT_NAME":            "agent-1",
		"BUILDKITE_AGENT_META_DATA_QUEUE": "default",
		"BUILDKITE_ORGANIZATION_SLUG":     "acme",
		"BUILDKITE_PIPELINE_SLUG":         "app",
		"BUILDKITE_BRANCH":                "main",
		"BUILDKITE_JOB_ID":                "job-123",
		"BUILDKITE_BUILD_ID":              "build-456",
		"BUILDKITE_BUILD_NUMBER":          "42",
		"BUILDKITE_BUILD_URL":             "https://buildkite.com/acme/app/builds/42",
		"BUILDKITE_SOURCE":                "webhook",
		"BUILDKITE_RETRY_COUNT":           "1",
		"BUILDKITE_LABEL":                 ":go: test",
		"BUILDKITE_COMMIT":                "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
		"BUILDKITE_PULL_REQUEST":          "1234",
		"BUILDKITE_PULL_REQUEST_DRAFT":    "true",
		"BUILDKITE_BUILD_AUTHOR":          "Ada Lovelace",
	})

	attrs := OTelResourceAttributesFromEnv(environ)

	byKey := make(map[attribute.Key]attribute.Value, len(attrs))
	for _, attr := range attrs {
		byKey[attr.Key] = attr.Value
	}

	wantStrings := map[attribute.Key]string{
		"buildkite.agent":              "agent-1",
		"buildkite.queue":              "default",
		"buildkite.org":                "acme",
		"buildkite.pipeline":           "app",
		"buildkite.branch":             "main",
		"buildkite.job_id":             "job-123",
		"buildkite.job_label":          ":go: test",
		"buildkite.build_number":       "42",
		"buildkite.commit":             "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
		"buildkite.pull_request":       "1234",
		"buildkite.pull_request_draft": "true",
		"buildkite.build_author":       "Ada Lovelace",
	}
	for key, want := range wantStrings {
		if got := byKey[key].AsString(); got != want {
			t.Errorf("attribute %s = %q, want %q", key, got, want)
		}
	}

	if got, want := byKey["buildkite.retry"].AsInt64(), int64(1); got != want {
		t.Errorf("attribute buildkite.retry = %d, want %d", got, want)
	}
}

func TestGenericTracingExtrasPullRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "pull request build", value: "1234", want: "1234"},
		{name: "non pull request build", value: "false", want: "n/a"},
		{name: "empty value", value: "", want: "n/a"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			environ := env.FromMap(map[string]string{
				"BUILDKITE_PULL_REQUEST": test.value,
			})
			extras := genericTracingExtras(jobTracingValues{}, environ)

			if got := extras["buildkite.pull_request"]; got != test.want {
				t.Errorf("buildkite.pull_request = %v, want %v", got, test.want)
			}
		})
	}
}

func TestGenericTracingExtrasPullRequestDraft(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{
			name: "draft pull request",
			env:  map[string]string{"BUILDKITE_PULL_REQUEST": "1234", "BUILDKITE_PULL_REQUEST_DRAFT": "true"},
			want: "true",
		},
		{
			name: "ready pull request",
			env:  map[string]string{"BUILDKITE_PULL_REQUEST": "1234"},
			want: "false",
		},
		{
			name: "pull request with draft explicitly false",
			env:  map[string]string{"BUILDKITE_PULL_REQUEST": "1234", "BUILDKITE_PULL_REQUEST_DRAFT": "false"},
			want: "false",
		},
		{
			name: "non pull request build",
			env:  map[string]string{"BUILDKITE_PULL_REQUEST": "false"},
			want: "n/a",
		},
		{
			name: "non pull request build with stray draft value",
			env:  map[string]string{"BUILDKITE_PULL_REQUEST": "false", "BUILDKITE_PULL_REQUEST_DRAFT": "true"},
			want: "n/a",
		},
		{
			name: "empty pull request",
			env:  map[string]string{"BUILDKITE_PULL_REQUEST": "", "BUILDKITE_PULL_REQUEST_DRAFT": "true"},
			want: "n/a",
		},
		{
			name: "pull request unset",
			env:  map[string]string{"BUILDKITE_PULL_REQUEST_DRAFT": "true"},
			want: "n/a",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			extras := genericTracingExtras(jobTracingValues{}, env.FromMap(test.env))

			if got := extras["buildkite.pull_request_draft"]; got != test.want {
				t.Errorf("buildkite.pull_request_draft = %v, want %v", got, test.want)
			}
		})
	}
}

func TestGenericTracingExtrasMatchesEnvDerivedExtras(t *testing.T) {
	t.Parallel()

	environ := env.FromMap(map[string]string{
		"BUILDKITE_AGENT_NAME":            "agent-1",
		"BUILDKITE_AGENT_META_DATA_QUEUE": "default",
		"BUILDKITE_ORGANIZATION_SLUG":     "acme",
		"BUILDKITE_PIPELINE_SLUG":         "app",
		"BUILDKITE_BRANCH":                "main",
		"BUILDKITE_JOB_ID":                "job-123",
		"BUILDKITE_RETRY_COUNT":           "1",
	})

	e := &Executor{
		ExecutorConfig: ExecutorConfig{
			AgentName:        "agent-1",
			Queue:            "default",
			OrganizationSlug: "acme",
			PipelineSlug:     "app",
			Branch:           "main",
			JobID:            "job-123",
		},
	}

	fromExecutor := GenericTracingExtras(e, environ)
	fromEnv := genericTracingExtras(jobTracingValues{
		AgentName:        "agent-1",
		Queue:            "default",
		OrganizationSlug: "acme",
		PipelineSlug:     "app",
		Branch:           "main",
		JobID:            "job-123",
	}, environ)

	if diff := cmp.Diff(fromExecutor, fromEnv); diff != "" {
		t.Errorf("executor-derived and env-derived extras differ (-executor +env):\n%s", diff)
	}
}
