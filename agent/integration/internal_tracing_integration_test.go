package integration

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/buildkite/agent/v4/agent"
	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/osc7501"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

func TestBootstrapInternalTracing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX shell commands")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name                       string
		customer, internal, config bool
		customerSink               string
		failSink                   string
		failJob                    bool
		invalidCustomerHeaders     bool
	}{
		{name: "neither"},
		{name: "customer only", customer: true},
		{name: "slow customer only", customer: true, customerSink: "slow"},
		{name: "internal only", internal: true, config: true},
		{name: "internal only with invalid customer headers", internal: true, config: true, invalidCustomerHeaders: true},
		{name: "both", customer: true, internal: true, config: true},
		{name: "next job flag off", customer: true, config: true},
		{name: "flag without registration", internal: true},
		{name: "internal sink fails", customer: true, internal: true, config: true, failSink: "unavailable"},
		{name: "internal sink stalls", customer: true, internal: true, config: true, failSink: "timeout"},
		{name: "job fails", customer: true, internal: true, config: true, failJob: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			customer := newTraceSink(t, "Bearer customer-token", tc.customerSink)
			internal := newTraceSink(t, "Bearer internal-token", tc.failSink)
			root := t.TempDir()
			checkEnv := `test -z "${BUILDKITE_INTERNAL_TRACING_EXPORTER+x}" || exit 91
! grep -qi '^BUILDKITE_INTERNAL_TRACING_EXPORTER=' "$BUILDKITE_ENV_FILE" || exit 92
! grep -q '"BUILDKITE_INTERNAL_TRACING_EXPORTER":' "$BUILDKITE_ENV_JSON_FILE" || exit 92
`
			report := osc7501.Report{ID: "private-status-id", State: "working", Progress: -1, Msg: "private-osc-message"}
			command := checkEnv + fmt.Sprintf("printf '%%s' '%s'\necho WORKLOAD_RAN", report.Encode())
			if tc.failJob {
				command += "\nexit 7"
			}
			hooks := filepath.Join(root, "hooks")
			if err := os.Mkdir(hooks, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(hooks, "pre-command"), []byte("#!/bin/sh\n"+checkEnv+"echo HOOK_RAN\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			conf := agent.AgentConfiguration{
				BootstrapScript: fmt.Sprintf("%q bootstrap --phases command --no-job-api --disable-warnings-for job-api-disabled --cancel-signal SIGTERM --experiment program-status", executable),
				BuildPath:       root, HooksPath: hooks, LocalHooksEnabled: true,
				CommandEval: true, Shell: "/bin/sh -e -c", OpenTelemetryTracing: tc.customer,
				TelemetryServiceName:  "customer-service",
				GitCommitVerification: "off", GitMirrorCheckoutMode: "reference",
			}
			if tc.config {
				conf.InternalTracingExporter = &api.TracingExporter{
					Endpoint: internal.server.URL + "/v1/traces", Protocol: "http/protobuf",
					Headers: map[string]string{"Authorization": "Bearer internal-token"},
				}
			}
			j := &api.Job{
				ID: defaultJobID, ChunksMaxSizeBytes: 1024, Token: "bkaj_job-token",
				InternalTracing: tc.internal,
				TraceParent:     "00-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-bbbbbbbbbbbbbbbb-01",
				TraceState:      "customer=private-tracestate",
				Env: map[string]string{
					"BUILDKITE_COMMAND": command, "BUILDKITE_JOB_ID": defaultJobID,
					"BUILDKITE_REPO": ".", "BUILDKITE_COMMIT": "HEAD", "BUILDKITE_BRANCH": "private-branch",
					"BUILDKITE_AGENT_NAME": "test-agent", "BUILDKITE_ORGANIZATION_SLUG": "private-org",
					"BUILDKITE_PIPELINE_SLUG": "private-pipeline", "BUILDKITE_PIPELINE_PROVIDER": "custom",
					"BUILDKITE_BUILD_CHECKOUT_PATH": root, "BUILDKITE_BUILD_AUTHOR": "private-author",
					"BUILDKITE_LABEL": "private-label", "BUILDKITE_AGENT_META_DATA_CUSTOM": "private-metadata",
					"OTEL_RESOURCE_ATTRIBUTES":            "custom=private-resource",
					"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT":  customer.server.URL + "/v1/traces",
					"OTEL_EXPORTER_OTLP_TRACES_PROTOCOL":  "http/protobuf",
					"OTEL_EXPORTER_OTLP_TRACES_HEADERS":   "Authorization=Bearer%20customer-token",
					"BUILDKITE_INTERNAL_TRACING_EXPORTER": "job-supplied-untrusted-config",
				},
			}
			if tc.invalidCustomerHeaders {
				j.Env["OTEL_EXPORTER_OTLP_HEADERS"] = "Authorization=Bearer%20customer-env-secret%ZZ"
				j.Env["OTEL_EXPORTER_OTLP_TRACES_HEADERS"] = "Authorization=Bearer%20customer-env-secret%ZZ"
			}
			e := createTestAgentEndpoint()
			server := e.server()
			defer server.Close()
			// Allow startup time in addition to the CLI's five-second trace flush
			// budget, but reject waiting for the SDK's longer export timeout.
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			if err := runJob(t, ctx, testRunJobConfig{job: j, server: server, agentCfg: conf}); err != nil {
				t.Fatal(err)
			}
			if err := ctx.Err(); err != nil {
				t.Fatalf("collector failure delayed job completion beyond the trace flush budget: %v", err)
			}
			logs := e.logsFor(t, defaultJobID)
			wantExit := "0"
			if tc.failJob {
				wantExit = "7"
			}
			if finish := e.finishesFor(t, defaultJobID)[0]; finish.ExitStatus != wantExit {
				t.Fatalf("job exit = %q, want %q\n%s", finish.ExitStatus, wantExit, logs)
			}
			if !strings.Contains(logs, "WORKLOAD_RAN") || !strings.Contains(logs, "HOOK_RAN") || strings.Contains(logs, "internal-token") || strings.Contains(logs, "customer-env-secret") {
				t.Fatalf("unexpected job log: %s", logs)
			}
			customerSpans := customer.spans()
			internalSpans := internal.spans()
			if got := len(customerSpans) > 0; got != tc.customer {
				t.Fatalf("customer spans present = %t, want %t", got, tc.customer)
			}
			if got, want := len(internalSpans) > 0, tc.internal && tc.config; got != want {
				t.Fatalf("internal spans present = %t, want %t", got, want)
			}
			if tc.customer {
				body := customer.body()
				for _, want := range []string{"private-author", "private-branch", "private-label", "private-metadata", "private-resource", "private-tracestate", "private-osc-message"} {
					if !bytes.Contains(body, []byte(want)) {
						t.Errorf("customer export lost %q", want)
					}
				}
			}
			for _, span := range internalSpans {
				if got := fmt.Sprintf("%x", span.TraceId); got != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
					t.Errorf("internal trace ID = %s, want server trace ID", got)
				}
				if tc.customer {
					other := customerSpans[string(span.SpanId)]
					if other == nil || !bytes.Equal(other.TraceId, span.TraceId) || !bytes.Equal(other.ParentSpanId, span.ParentSpanId) || other.StartTimeUnixNano != span.StartTimeUnixNano || other.EndTimeUnixNano != span.EndTimeUnixNano || other.Status.GetCode() != span.Status.GetCode() {
						t.Errorf("internal span does not match customer span: %v / %v", span, other)
					}
				}
				if len(span.Attributes) != 0 || len(span.Events) != 0 || len(span.Links) != 0 || span.Status.GetMessage() != "" || span.TraceState != "" {
					t.Errorf("internal span contains customer detail: %v", span)
				}
			}
			for _, banned := range []string{"private-", "customer-token", "internal-token", "customer-service", "job-supplied-untrusted-config"} {
				if bytes.Contains(internal.body(), []byte(banned)) {
					t.Errorf("internal export contains %q", banned)
				}
			}
		})
	}
}

type traceSink struct {
	server   *httptest.Server
	mu       sync.Mutex
	requests []*collector.ExportTraceServiceRequest
}

func newTraceSink(t *testing.T, auth, fail string) *traceSink {
	t.Helper()
	s := &traceSink{}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/traces" || r.Header.Get("Authorization") != auth || r.Header.Get("Content-Type") != "application/x-protobuf" {
			t.Errorf("unexpected OTLP request: %s %s auth=%q content-type=%q", r.Method, r.URL, r.Header.Get("Authorization"), r.Header.Get("Content-Type"))
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		var request collector.ExportTraceServiceRequest
		if err := proto.Unmarshal(body, &request); err != nil {
			t.Error(err)
			return
		}
		if fail == "slow" {
			// Bootstrap must not impose a five-second deadline on the
			// customer's final export when internal tracing is disabled.
			timer := time.NewTimer(6 * time.Second)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-r.Context().Done():
				return
			}
		}
		s.mu.Lock()
		s.requests = append(s.requests, &request)
		s.mu.Unlock()
		if fail == "unavailable" {
			http.Error(w, "private-sink-error internal-token", http.StatusServiceUnavailable)
			return
		}
		if fail == "timeout" {
			<-r.Context().Done()
			return
		}
		w.Header().Set("Content-Type", "application/x-protobuf")
	}))
	t.Cleanup(s.server.Close)
	return s
}

func (s *traceSink) spans() map[string]*tracepb.Span {
	s.mu.Lock()
	defer s.mu.Unlock()
	spans := make(map[string]*tracepb.Span)
	for _, request := range s.requests {
		for _, resource := range request.ResourceSpans {
			for _, scope := range resource.ScopeSpans {
				for _, span := range scope.Spans {
					spans[string(span.SpanId)] = span
				}
			}
		}
	}
	return spans
}

func (s *traceSink) body() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	var body []byte
	for _, request := range s.requests {
		data, _ := proto.Marshal(request)
		body = append(body, data...)
	}
	return body
}
