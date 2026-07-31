package agent

import (
	"os"
	"slices"
	"strings"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/env"
)

// ControlPlaneOTLPTracingFeature is advertised at registration when this agent
// binary can safely consume job.tracing_exporter (apply to bootstrap-only env
// and keep credentials out of the command environment / env files).
const ControlPlaneOTLPTracingFeature = "control-plane-otlp-tracing"

// hostHasOTelExporterConfig reports whether the agent process already has local
// OTLP exporter configuration. Local operator config takes precedence over
// control-plane injection.
func hostHasOTelExporterConfig() bool {
	for _, key := range env.OTelExporterKeys {
		if os.Getenv(key) != "" {
			return true
		}
	}
	return false
}

// scrubPipelineOTelExporterEnv removes OTLP exporter vars from the job env map
// so a pipeline cannot choose a destination or supply auth headers. Returns the
// names that were present (for BUILDKITE_IGNORED_ENV).
func scrubPipelineOTelExporterEnv(jobEnv map[string]string) []string {
	var ignored []string
	for _, key := range env.OTelExporterKeys {
		if _, ok := jobEnv[key]; ok {
			delete(jobEnv, key)
			ignored = append(ignored, key)
		}
	}
	return ignored
}

// encodeOTLPHeaders formats a header map as OTEL_EXPORTER_OTLP_HEADERS expects:
// comma-separated key=value pairs. Keys are sorted for stable output.
func encodeOTLPHeaders(headers map[string]string) string {
	if len(headers) == 0 {
		return ""
	}
	keys := make([]string, 0, len(headers))
	for k, v := range headers {
		if k == "" || v == "" {
			continue
		}
		keys = append(keys, k)
	}
	slices.Sort(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+headers[k])
	}
	return strings.Join(parts, ",")
}

// applyControlPlaneTracingExporter sets bootstrap-only OTEL_* vars from the job
// payload when the control plane supplied tracing_exporter and the agent host
// has not already configured an exporter. Call this after writing
// BUILDKITE_ENV_FILE so secrets are not persisted there.
func applyControlPlaneTracingExporter(setEnv func(name, value string), exporter *api.TracingExporter) {
	if exporter == nil || exporter.Endpoint == "" {
		return
	}
	if hostHasOTelExporterConfig() {
		return
	}

	protocol := exporter.Protocol
	if protocol == "" {
		protocol = "http/protobuf"
	}
	setEnv("OTEL_EXPORTER_OTLP_PROTOCOL", protocol)
	// Full traces URL from the control plane (includes /v1/traces).
	setEnv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", exporter.Endpoint)

	if encoded := encodeOTLPHeaders(exporter.Headers); encoded != "" {
		setEnv("OTEL_EXPORTER_OTLP_HEADERS", encoded)
	}
}
