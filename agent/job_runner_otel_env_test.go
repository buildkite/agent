package agent

import (
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/agent/v3/tracetools"
)

// jobEnvForTest runs createEnvironment and returns the resulting environment.
func jobEnvForTest(t *testing.T, agentConf AgentConfiguration, jobEnv map[string]string) *env.Environment {
	t.Helper()

	l := logger.Discard
	runner := &JobRunner{
		agentLogger: l,
		apiClient:   api.NewClient(l, api.Config{}),
		conf: JobRunnerConfig{
			Job:                &api.Job{ID: "job-1", Env: jobEnv},
			AgentConfiguration: agentConf,
		},
	}

	envSlice, err := runner.createEnvironment(t.Context())
	if err != nil {
		t.Fatalf("createEnvironment() error = %v", err)
	}
	return env.FromSlice(envSlice)
}

func otelAgentConfWithExporter() AgentConfiguration {
	return AgentConfiguration{
		TracingBackend: tracetools.BackendOpenTelemetry,
		TracingExporter: &api.TracingExporter{
			Endpoint: "https://otel.example/v1/traces",
			Protocol: "http/protobuf",
			Headers:  map[string]string{"Authorization": "Bearer from-cp"},
		},
	}
}

// With control-plane exporter config, the agent's values win and the pipeline's
// are reported as ignored.
func TestCreateEnvironmentAppliesControlPlaneExporter(t *testing.T) {
	for _, key := range env.OTelExporterKeys {
		t.Setenv(key, "")
	}

	got := jobEnvForTest(t, otelAgentConfWithExporter(), map[string]string{
		"OTEL_EXPORTER_OTLP_ENDPOINT":        "https://evil.example",
		"OTEL_EXPORTER_OTLP_TRACES_HEADERS":  "Authorization=Bearer pipeline",
		"OTEL_EXPORTER_OTLP_TRACES_INSECURE": "true",
	})

	for key, want := range map[string]string{
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT": "https://otel.example/v1/traces",
		"OTEL_EXPORTER_OTLP_TRACES_PROTOCOL": "http/protobuf",
		"OTEL_EXPORTER_OTLP_TRACES_HEADERS":  "Authorization=Bearer%20from-cp",
		env.OTelExporterMarkerEnv:            "true",
	} {
		if v, _ := got.Get(key); v != want {
			t.Errorf("%s = %q, want %q", key, v, want)
		}
	}

	// The pipeline's own destination and TLS settings are gone, not merged.
	for _, key := range []string{
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_EXPORTER_OTLP_TRACES_INSECURE",
	} {
		if v, ok := got.Get(key); ok {
			t.Errorf("%s = %q, want it scrubbed", key, v)
		}
	}

	ignored, _ := got.Get("BUILDKITE_IGNORED_ENV")
	for _, key := range []string{"OTEL_EXPORTER_OTLP_ENDPOINT", "OTEL_EXPORTER_OTLP_TRACES_HEADERS"} {
		if !strings.Contains(ignored, key) {
			t.Errorf("BUILDKITE_IGNORED_ENV = %q, want it to mention %s", ignored, key)
		}
	}
}

// Without control-plane exporter config there is no organisation credential in
// the job environment, so a pipeline keeps whatever exporter env it sets. This
// is the case for every agent that does not use this feature.
func TestCreateEnvironmentLeavesPipelineExporterConfigAlone(t *testing.T) {
	pipelineEnv := map[string]string{
		"OTEL_EXPORTER_OTLP_ENDPOINT": "https://our-own-collector.example",
		"OTEL_EXPORTER_OTLP_HEADERS":  "Authorization=Bearer ours",
	}

	for _, test := range []struct {
		name string
		conf AgentConfiguration
	}{
		{
			name: "no tracing configured",
			conf: AgentConfiguration{},
		},
		{
			name: "opentelemetry backend without control-plane exporter",
			conf: AgentConfiguration{TracingBackend: tracetools.BackendOpenTelemetry},
		},
		{
			name: "non-opentelemetry backend with an exporter",
			conf: func() AgentConfiguration {
				conf := otelAgentConfWithExporter()
				conf.TracingBackend = tracetools.BackendDatadog
				return conf
			}(),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, key := range env.OTelExporterKeys {
				t.Setenv(key, "")
			}

			got := jobEnvForTest(t, test.conf, pipelineEnv)

			for key, want := range pipelineEnv {
				if v, _ := got.Get(key); v != want {
					t.Errorf("%s = %q, want %q", key, v, want)
				}
			}
			if v, ok := got.Get(env.OTelExporterMarkerEnv); ok {
				t.Errorf("%s = %q, want it unset", env.OTelExporterMarkerEnv, v)
			}
		})
	}
}

// A host exporter configuration is the operator's choice and must not be
// replaced by the control plane's.
func TestCreateEnvironmentHostExporterConfigWins(t *testing.T) {
	for _, key := range env.OTelExporterKeys {
		t.Setenv(key, "")
	}
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "https://operator-collector.example")

	got := jobEnvForTest(t, otelAgentConfWithExporter(), nil)

	if v, ok := got.Get("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"); ok {
		t.Errorf("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT = %q, want no control-plane endpoint", v)
	}
	if v, ok := got.Get(env.OTelExporterMarkerEnv); ok {
		t.Errorf("%s = %q, want it unset", env.OTelExporterMarkerEnv, v)
	}
}

// A pipeline must not be able to make bootstrap strip the operator's own
// exporter configuration by supplying the marker itself.
func TestCreateEnvironmentDropsPipelineSuppliedMarker(t *testing.T) {
	for _, key := range env.OTelExporterKeys {
		t.Setenv(key, "")
	}

	got := jobEnvForTest(t, AgentConfiguration{}, map[string]string{
		env.OTelExporterMarkerEnv: "true",
	})

	if v, ok := got.Get(env.OTelExporterMarkerEnv); ok {
		t.Errorf("%s = %q, want it dropped", env.OTelExporterMarkerEnv, v)
	}
}
