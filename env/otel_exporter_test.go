package env

import (
	"os"
	"strings"
	"testing"
)

func TestScrubOTelExporterKeys(t *testing.T) {
	t.Parallel()

	jobEnv := map[string]string{
		"BUILDKITE_COMMAND":           "echo hi",
		"OTEL_EXPORTER_OTLP_ENDPOINT": "https://evil.example",
		"OTEL_EXPORTER_OTLP_HEADERS":  "Authorization=Bearer pipeline",
		"OTEL_RESOURCE_ATTRIBUTES":    "keep=me",
	}
	scrubbed := ScrubOTelExporterKeys(jobEnv)

	if len(scrubbed) != 2 {
		t.Errorf("scrubbed = %v, want 2 keys", scrubbed)
	}
	if len(jobEnv) != 2 || jobEnv["OTEL_RESOURCE_ATTRIBUTES"] != "keep=me" {
		t.Errorf("jobEnv = %#v", jobEnv)
	}
}

// Windows environment names are case-insensitive, so an exact-key match there
// could be sidestepped with different casing. normalizeKeyName is GOOS-dependent,
// so drive scrubKeys with the Windows normalizer directly to cover it anywhere.
func TestScrubKeysUnderCaseInsensitiveNormalization(t *testing.T) {
	t.Parallel()

	jobEnv := map[string]string{
		"otel_exporter_otlp_endpoint": "https://evil.example",
		"Otel_Exporter_Otlp_Headers":  "Authorization=Bearer pipeline",
		"BUILDKITE_COMMAND":           "echo hi",
	}
	scrubbed := scrubKeys(jobEnv, OTelExporterKeys, strings.ToUpper)

	if len(scrubbed) != 2 {
		t.Errorf("scrubbed = %v, want both mixed-case exporter keys", scrubbed)
	}
	if len(jobEnv) != 1 || jobEnv["BUILDKITE_COMMAND"] != "echo hi" {
		t.Errorf("jobEnv = %#v", jobEnv)
	}
}

func TestStripInjectedOTelExporter(t *testing.T) {
	t.Parallel()

	t.Run("removes injected values and the marker when marked", func(t *testing.T) {
		t.Parallel()

		environ := FromSlice([]string{
			"PATH=/bin",
			OTelExporterMarkerEnv + "=true",
			"OTEL_EXPORTER_OTLP_TRACES_HEADERS=Authorization=Bearer secret",
			"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=https://otel.example/v1/traces",
			"OTEL_EXPORTER_OTLP_TRACES_PROTOCOL=http/protobuf",
			"BUILDKITE_JOB_ID=abc",
		})
		StripInjectedOTelExporter(environ)

		for _, key := range append(OTelExporterInjectedKeys, OTelExporterMarkerEnv) {
			if _, ok := environ.Get(key); ok {
				t.Errorf("expected %s to be stripped", key)
			}
		}
		if v, _ := environ.Get("BUILDKITE_JOB_ID"); v != "abc" {
			t.Errorf("BUILDKITE_JOB_ID = %q", v)
		}
	})

	// The operator's own exporter config must reach hooks and the command.
	t.Run("leaves everything alone without the marker", func(t *testing.T) {
		t.Parallel()

		environ := FromSlice([]string{
			"OTEL_EXPORTER_OTLP_ENDPOINT=https://operator-collector.example",
			"OTEL_EXPORTER_OTLP_HEADERS=Authorization=Bearer operator-own",
			"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=https://operator-traces.example",
		})
		StripInjectedOTelExporter(environ)

		if environ.Length() != 3 {
			t.Errorf("environ = %v, want operator config untouched", environ.ToSlice())
		}
	})

	t.Run("nil environ", func(t *testing.T) {
		t.Parallel()
		StripInjectedOTelExporter(nil)
	})
}

func TestClearInjectedOTelExporterFromProcess(t *testing.T) {
	t.Run("clears when marked", func(t *testing.T) {
		t.Setenv(OTelExporterMarkerEnv, "true")
		t.Setenv("OTEL_EXPORTER_OTLP_TRACES_HEADERS", "Authorization=Bearer secret")
		t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "https://operator-collector.example")

		ClearInjectedOTelExporterFromProcess()

		if _, ok := os.LookupEnv("OTEL_EXPORTER_OTLP_TRACES_HEADERS"); ok {
			t.Error("expected injected headers to be cleared")
		}
		if _, ok := os.LookupEnv(OTelExporterMarkerEnv); ok {
			t.Error("expected the marker to be cleared")
		}
		// Only injected keys are cleared, so generic operator config survives for
		// anything later in the process that reads it.
		if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "https://operator-collector.example" {
			t.Error("expected operator config to be left alone")
		}
	})

	t.Run("no-op without the marker", func(t *testing.T) {
		t.Setenv("OTEL_EXPORTER_OTLP_TRACES_HEADERS", "Authorization=Bearer operator-own")

		ClearInjectedOTelExporterFromProcess()

		if os.Getenv("OTEL_EXPORTER_OTLP_TRACES_HEADERS") != "Authorization=Bearer operator-own" {
			t.Error("expected operator config to survive without the marker")
		}
	})
}
