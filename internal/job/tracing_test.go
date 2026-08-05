package job

import (
	"testing"

	"github.com/buildkite/agent/v4/env"
	"github.com/google/go-cmp/cmp"
	"go.opentelemetry.io/otel/attribute"
)

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
	})

	attrs := OTelResourceAttributesFromEnv(environ)

	byKey := make(map[attribute.Key]attribute.Value, len(attrs))
	for _, attr := range attrs {
		byKey[attr.Key] = attr.Value
	}

	wantStrings := map[attribute.Key]string{
		"buildkite.agent":        "agent-1",
		"buildkite.queue":        "default",
		"buildkite.org":          "acme",
		"buildkite.pipeline":     "app",
		"buildkite.branch":       "main",
		"buildkite.job_id":       "job-123",
		"buildkite.job_label":    ":go: test",
		"buildkite.build_number": "42",
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
