package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/logger"
	"github.com/google/go-cmp/cmp"
)

const testBearerToken = "Bearer super-secret-token"

func TestApplyControlPlaneOTel(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name         string
		policy       string
		conf         AgentConfiguration
		localTracing LocalTracingConfig
		localLogs    LocalJobLogsConfig
		wantTracing  bool
		wantLogs     bool
		wantTracesEP string
		wantLogsEP   string
	}{
		{name: "legacy fallback", wantTracing: true, wantTracesEP: "https://collector.example/v1/traces"},
		{name: "disabled shared policy suppresses legacy", policy: `{"enabled":false,"job_logs":true}`},
		{name: "empty shared policy suppresses legacy", policy: `{}`},
		{name: "disabled shared policy preserves local opt-ins", policy: `{"enabled":false}`, conf: AgentConfiguration{OpenTelemetryTracing: true, JobLogsOTLP: true}, wantTracing: true, wantLogs: true},
		{name: "job logs defaults false", policy: `{"enabled":true,"exporter":{"endpoint":"https://shared.example/prefix"}}`, wantTracing: true, wantTracesEP: "https://shared.example/prefix/v1/traces"},
		{name: "shared exporter serves both signals", policy: `{"enabled":true,"job_logs":true,"exporter":{"endpoint":"https://shared.example/prefix/","protocol":"http/protobuf","headers":{"Authorization":"Bearer super-secret-token"}}}`, wantTracing: true, wantLogs: true, wantTracesEP: "https://shared.example/prefix/v1/traces", wantLogsEP: "https://shared.example/prefix/v1/logs"},
		{name: "local traces opt-out does not disable logs", policy: `{"enabled":true,"job_logs":true}`, localTracing: LocalTracingConfig{EnabledSet: true}, wantLogs: true},
		{name: "local logs opt-out does not disable traces", policy: `{"enabled":true,"job_logs":true}`, localLogs: LocalJobLogsConfig{EnabledSet: true}, wantTracing: true},
		{name: "local trace destination blocks shared tuple only for traces", policy: `{"enabled":true,"job_logs":true,"exporter":{"endpoint":"https://shared.example"}}`, localTracing: LocalTracingConfig{OTLPDestinationSet: true}, wantTracing: true, wantLogs: true, wantLogsEP: "https://shared.example/v1/logs"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reg := api.AgentRegisterResponse{Tracing: serverTracing()}
			if tc.policy != "" {
				if err := json.Unmarshal([]byte(tc.policy), &reg.OTel); err != nil {
					t.Fatal(err)
				}
			}
			conf := tc.conf
			log := logger.NewBuffer()
			ApplyControlPlaneOTel(log, &conf, &reg, tc.localTracing, tc.localLogs)
			if conf.OpenTelemetryTracing != tc.wantTracing || conf.JobLogsOTLP != tc.wantLogs {
				t.Errorf("enabled traces/logs = %t/%t, want %t/%t", conf.OpenTelemetryTracing, conf.JobLogsOTLP, tc.wantTracing, tc.wantLogs)
			}
			for signal, check := range map[string]struct {
				exporter *api.OTLPExporter
				endpoint string
			}{
				"TRACES": {conf.ControlPlaneTracingExporter, tc.wantTracesEP},
				"LOGS":   {conf.ControlPlaneJobLogsExporter, tc.wantLogsEP},
			} {
				if check.endpoint == "" {
					if check.exporter != nil {
						t.Errorf("unexpected %s exporter", signal)
					}
					continue
				}
				if check.exporter == nil {
					t.Fatalf("missing %s exporter", signal)
				}
				got := controlPlaneOTLPEnv(check.exporter, signal)
				if got["OTEL_EXPORTER_OTLP_"+signal+"_ENDPOINT"] != check.endpoint {
					t.Errorf("%s endpoint = %q, want %q", signal, check.exporter.Endpoint, check.endpoint)
				}
				if reg.OTel != nil && reg.OTel.Exporter != nil {
					if diff := cmp.Diff(reg.OTel.Exporter.Headers, check.exporter.Headers); diff != "" || reg.OTel.Exporter.Protocol != check.exporter.Protocol {
						t.Errorf("%s exporter tuple changed: %s", signal, diff)
					}
				}
			}
			for _, msg := range log.Messages {
				if strings.Contains(msg, testBearerToken) || strings.Contains(msg, "/prefix") {
					t.Errorf("exporter credential/path leaked: %s", msg)
				}
			}
		})
	}
}

func serverTracing() *api.AgentTracing {
	return &api.AgentTracing{
		Backend:              "opentelemetry",
		PropagateTraceparent: true,
		Exporter: &api.OTLPExporter{
			Endpoint: "https://collector.example/v1/traces",
			Protocol: "http/protobuf",
			Headers:  map[string]string{"Authorization": testBearerToken},
		},
	}
}

func TestApplyControlPlaneTracing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		conf            AgentConfiguration
		tracing         *api.AgentTracing
		local           LocalTracingConfig
		wantOTelTracing bool
		wantExporter    bool
	}{
		{
			name:    "nil tracing is a no-op",
			tracing: nil,
		},
		{
			name:            "no local config applies backend, propagation and exporter",
			tracing:         serverTracing(),
			wantOTelTracing: true,
			wantExporter:    true,
		},
		{
			name:            "local otel backend without destination consumes the exporter",
			conf:            AgentConfiguration{OpenTelemetryTracing: true},
			tracing:         serverTracing(),
			wantOTelTracing: true,
			wantExporter:    true,
		},
		{
			name:            "local OTLP destination blocks the exporter but not the policy",
			tracing:         serverTracing(),
			local:           LocalTracingConfig{OTLPDestinationSet: true},
			wantOTelTracing: true,
			wantExporter:    false,
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
			name: "exporter without endpoint is not applied",
			tracing: &api.AgentTracing{
				Backend:  "opentelemetry",
				Exporter: &api.OTLPExporter{Protocol: "http/protobuf"},
			},
			wantOTelTracing: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			conf := tc.conf
			buf := logger.NewBuffer()
			ApplyControlPlaneTracing(buf, &conf, tc.tracing, tc.local)

			if got, want := conf.OpenTelemetryTracing, tc.wantOTelTracing; got != want {
				t.Errorf("conf.OpenTelemetryTracing = %t, want %t", got, want)
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

	got := otlpHeaderValue(map[string]string{
		"X-Later":       "plain",
		"Authorization": "Bearer abc,def+ghi/jk=",
		"A-First":       "with space",
	})
	// Keys sorted; values path-escaped (spaces, commas and slashes escaped),
	// '+' escaped as %2B for form-style SDK parsers, keys untouched.
	want := "A-First=with%20space,Authorization=Bearer%20abc%2Cdef%2Bghi%2Fjk=,X-Later=plain"
	if got != want {
		t.Errorf("otlpHeaderValue() = %q, want %q", got, want)
	}

	if got := otlpHeaderValue(nil); got != "" {
		t.Errorf("otlpHeaderValue(nil) = %q, want empty", got)
	}
}

func TestControlPlaneOTLPEnv(t *testing.T) {
	t.Parallel()

	got := controlPlaneOTLPEnv(serverTracing().Exporter, "TRACES")

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
	got = controlPlaneOTLPEnv(&api.OTLPExporter{Endpoint: "https://collector.example"}, "TRACES")
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
