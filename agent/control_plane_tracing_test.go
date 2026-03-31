package agent

import (
	"strings"
	"testing"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/logger"
	"github.com/google/go-cmp/cmp"
)

const testBearerToken = "Bearer super-secret-token"

func serverTracing() *api.AgentTracing {
	return &api.AgentTracing{
		Backend:              "opentelemetry",
		PropagateTraceparent: true,
		Exporter: &api.TracingExporter{
			Endpoint: "https://collector.example/v1/traces",
			Protocol: "http/protobuf",
			Headers:  map[string]string{"Authorization": testBearerToken},
		},
	}
}

func TestApplyControlPlaneTracing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		conf          AgentConfiguration
		tracing       *api.AgentTracing
		local         LocalTracingConfig
		wantBackend   string
		wantPropagate bool
		wantExporter  bool
	}{
		{
			name:    "nil tracing is a no-op",
			tracing: nil,
		},
		{
			name:          "no local config applies backend, propagation and exporter",
			tracing:       serverTracing(),
			wantBackend:   "opentelemetry",
			wantPropagate: true,
			wantExporter:  true,
		},
		{
			name:        "local datadog backend ignores server policy entirely",
			conf:        AgentConfiguration{TracingBackend: "datadog"},
			tracing:     serverTracing(),
			wantBackend: "datadog",
		},
		{
			name:          "local otel backend without destination consumes the exporter",
			conf:          AgentConfiguration{TracingBackend: "opentelemetry"},
			tracing:       serverTracing(),
			wantBackend:   "opentelemetry",
			wantPropagate: true,
			wantExporter:  true,
		},
		{
			name:          "local OTLP destination blocks the exporter but not the policy",
			tracing:       serverTracing(),
			local:         LocalTracingConfig{OTLPDestinationSet: true},
			wantBackend:   "opentelemetry",
			wantPropagate: true,
			wantExporter:  false,
		},
		{
			name:          "explicit local propagate_traceparent=false wins",
			tracing:       serverTracing(),
			local:         LocalTracingConfig{PropagateTraceparentSet: true},
			wantBackend:   "opentelemetry",
			wantPropagate: false,
			wantExporter:  true,
		},
		{
			name: "unsupported server backend is ignored entirely",
			tracing: &api.AgentTracing{
				Backend:              "quantum-entanglement",
				PropagateTraceparent: true,
				Exporter:             serverTracing().Exporter,
			},
		},
		{
			name: "server datadog backend is ignored even with local otel backend",
			conf: AgentConfiguration{TracingBackend: "opentelemetry"},
			tracing: &api.AgentTracing{
				Backend:              "datadog",
				PropagateTraceparent: true,
				Exporter:             serverTracing().Exporter,
			},
			wantBackend: "opentelemetry",
		},
		{
			name: "exporter without endpoint is not applied",
			tracing: &api.AgentTracing{
				Backend:  "opentelemetry",
				Exporter: &api.TracingExporter{Protocol: "http/protobuf"},
			},
			wantBackend: "opentelemetry",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			conf := tc.conf
			buf := logger.NewBuffer()
			ApplyControlPlaneTracing(buf, &conf, tc.tracing, tc.local)

			if got, want := conf.TracingBackend, tc.wantBackend; got != want {
				t.Errorf("conf.TracingBackend = %q, want %q", got, want)
			}
			if got, want := conf.TracingPropagateTraceparent, tc.wantPropagate; got != want {
				t.Errorf("conf.TracingPropagateTraceparent = %t, want %t", got, want)
			}
			if got, want := conf.ControlPlaneTracingExporter != nil, tc.wantExporter; got != want {
				t.Errorf("conf.ControlPlaneTracingExporter set = %t, want %t", got, want)
			}

			// Header credentials and full endpoint paths must never be logged.
			for _, msg := range buf.Messages {
				if strings.Contains(msg, testBearerToken) {
					t.Errorf("log message contains exporter header credential: %q", msg)
				}
				if strings.Contains(msg, "/v1/traces") {
					t.Errorf("log message contains exporter endpoint path: %q", msg)
				}
			}
		})
	}
}

func TestOTLPTracesHeaderValue(t *testing.T) {
	t.Parallel()

	got := otlpTracesHeaderValue(map[string]string{
		"X-Later":       "plain",
		"Authorization": "Bearer abc,def+ghi/jk=",
		"A-First":       "with space",
	})
	// Keys sorted; values path-escaped (spaces, commas and slashes escaped),
	// '+' escaped as %2B for form-style SDK parsers, keys untouched.
	want := "A-First=with%20space,Authorization=Bearer%20abc%2Cdef%2Bghi%2Fjk=,X-Later=plain"
	if got != want {
		t.Errorf("otlpTracesHeaderValue() = %q, want %q", got, want)
	}

	if got := otlpTracesHeaderValue(nil); got != "" {
		t.Errorf("otlpTracesHeaderValue(nil) = %q, want empty", got)
	}
}

func TestControlPlaneOTLPEnv(t *testing.T) {
	t.Parallel()

	got := controlPlaneOTLPEnv(serverTracing().Exporter)

	want := map[string]string{
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT": "https://collector.example/v1/traces",
		"OTEL_EXPORTER_OTLP_TRACES_PROTOCOL": "http/protobuf",
		"OTEL_EXPORTER_OTLP_TRACES_HEADERS":  "Authorization=Bearer%20super-secret-token",
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("controlPlaneOTLPEnv() diff (-got +want):\n%s", diff)
	}

	// Absent protocol/headers are omitted, not injected as empty-valued vars:
	// the OTel spec treats empty as unset, but not every SDK does.
	got = controlPlaneOTLPEnv(&api.TracingExporter{Endpoint: "https://collector.example"})
	want = map[string]string{
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT": "https://collector.example",
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("controlPlaneOTLPEnv() with endpoint only, diff (-got +want):\n%s", diff)
	}
}

func TestSanitizedEndpoint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		endpoint string
		want     string
	}{
		{"https://collector.example/v1/traces?api_key=secret", "https://collector.example"},
		{"https://user:pass@collector.example/v1/traces", "https://collector.example"},
		{"http://[2001:db8::1]:4318/v1/traces", "http://[2001:db8::1]:4318"},
		{"collector.example", "(unparseable endpoint)"},
		{"://nope", "(unparseable endpoint)"},
	}
	for _, tc := range tests {
		if got := sanitizedEndpoint(tc.endpoint); got != tc.want {
			t.Errorf("sanitizedEndpoint(%q) = %q, want %q", tc.endpoint, got, tc.want)
		}
	}
}
