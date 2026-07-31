package agent

import (
	"testing"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/env"
)

func TestEncodeOTLPHeaders(t *testing.T) {
	t.Parallel()

	got := encodeOTLPHeaders(map[string]string{
		"X-Team":        "pipelines",
		"Authorization": "Bearer secret",
		"":              "skip",
		"Empty":         "",
	})
	want := "Authorization=Bearer secret,X-Team=pipelines"
	if got != want {
		t.Fatalf("encodeOTLPHeaders() = %q, want %q", got, want)
	}
}

func TestScrubPipelineOTelExporterEnv(t *testing.T) {
	t.Parallel()

	jobEnv := map[string]string{
		"BUILDKITE_COMMAND":           "echo hi",
		"OTEL_EXPORTER_OTLP_HEADERS":  "Authorization=Bearer pipeline",
		"OTEL_EXPORTER_OTLP_ENDPOINT": "https://evil.example",
		"OTEL_EXPORTER_OTLP_PROTOCOL": "grpc",
		"OTEL_RESOURCE_ATTRIBUTES":    "keep=me",
	}
	ignored := scrubPipelineOTelExporterEnv(jobEnv)

	if _, ok := jobEnv["OTEL_EXPORTER_OTLP_HEADERS"]; ok {
		t.Fatal("expected OTEL_EXPORTER_OTLP_HEADERS to be scrubbed")
	}
	if jobEnv["OTEL_RESOURCE_ATTRIBUTES"] != "keep=me" {
		t.Fatal("expected non-exporter OTEL vars to remain")
	}
	if len(ignored) != 3 {
		t.Fatalf("ignored = %v, want 3 exporter keys", ignored)
	}
}

func TestApplyControlPlaneTracingExporter(t *testing.T) {
	// Uses t.Setenv — cannot run in parallel with other tests that touch process env.
	t.Run("applies when host has no OTEL config", func(t *testing.T) {
		for _, key := range env.OTelExporterKeys {
			t.Setenv(key, "")
		}

		got := map[string]string{}
		setEnv := func(name, value string) { got[name] = value }

		applyControlPlaneTracingExporter(setEnv, &api.TracingExporter{
			Endpoint: "https://otel.example/v1/traces",
			Protocol: "http/protobuf",
			Headers:  map[string]string{"Authorization": "Bearer from-cp"},
		})

		if got["OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"] != "https://otel.example/v1/traces" {
			t.Fatalf("endpoint = %q", got["OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"])
		}
		if got["OTEL_EXPORTER_OTLP_PROTOCOL"] != "http/protobuf" {
			t.Fatalf("protocol = %q", got["OTEL_EXPORTER_OTLP_PROTOCOL"])
		}
		if got["OTEL_EXPORTER_OTLP_HEADERS"] != "Authorization=Bearer from-cp" {
			t.Fatalf("headers = %q", got["OTEL_EXPORTER_OTLP_HEADERS"])
		}
	})

	t.Run("skips when host already configured", func(t *testing.T) {
		t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "https://local.example")

		got := map[string]string{}
		setEnv := func(name, value string) { got[name] = value }

		applyControlPlaneTracingExporter(setEnv, &api.TracingExporter{
			Endpoint: "https://otel.example/v1/traces",
			Headers:  map[string]string{"Authorization": "Bearer from-cp"},
		})

		if len(got) != 0 {
			t.Fatalf("expected no control-plane apply when host configured, got %#v", got)
		}
	})

	t.Run("skips nil or empty endpoint", func(t *testing.T) {
		got := map[string]string{}
		setEnv := func(name, value string) { got[name] = value }

		applyControlPlaneTracingExporter(setEnv, nil)
		applyControlPlaneTracingExporter(setEnv, &api.TracingExporter{})
		if len(got) != 0 {
			t.Fatalf("expected no apply, got %#v", got)
		}
	})
}

func TestStripOTelExporter(t *testing.T) {
	t.Parallel()

	environ := env.FromSlice([]string{
		"PATH=/bin",
		"OTEL_EXPORTER_OTLP_HEADERS=Authorization=Bearer secret",
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=https://otel.example/v1/traces",
		"BUILDKITE_JOB_ID=abc",
	})
	env.StripOTelExporter(environ)

	if _, ok := environ.Get("OTEL_EXPORTER_OTLP_HEADERS"); ok {
		t.Fatal("expected headers stripped")
	}
	if _, ok := environ.Get("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"); ok {
		t.Fatal("expected endpoint stripped")
	}
	if v, _ := environ.Get("BUILDKITE_JOB_ID"); v != "abc" {
		t.Fatalf("job id = %q", v)
	}
}
