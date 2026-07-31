package agent

import (
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/tracetools"
)

// ControlPlaneOTLPTracingFeature is advertised at registration when this agent
// binary can safely consume job.tracing_exporter (apply to bootstrap-only env
// and keep credentials out of the command environment / env files).
const ControlPlaneOTLPTracingFeature = "control-plane-otlp-tracing"

// ApplyRegistrationTracing merges non-secret control-plane policy into the
// worker's runtime config. Explicit local backend and propagation settings win.
func (c *AgentConfiguration) ApplyRegistrationTracing(
	tracing *api.AgentRegistrationTracing,
	backendLocallyConfigured bool,
	propagateTraceparentLocallyConfigured bool,
) {
	if tracing == nil || tracing.Backend != tracetools.BackendOpenTelemetry {
		return
	}

	if !backendLocallyConfigured {
		c.TracingBackend = tracing.Backend
	}
	if c.TracingBackend != tracetools.BackendOpenTelemetry {
		return
	}

	if !propagateTraceparentLocallyConfigured {
		c.TracingPropagateTraceparent = tracing.PropagateTraceparent
	}
	c.AcceptControlPlaneExporter = tracing.AcceptControlPlaneExporter
}

// hostHasOTelExporterConfig reports whether the agent process already has local
// OTLP endpoint or headers. Protocol-only leftovers do not count as a full
// local override. Local operator config takes precedence over control-plane
// injection when an endpoint or headers are set.
func hostHasOTelExporterConfig() bool {
	return os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" ||
		os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") != "" ||
		os.Getenv("OTEL_EXPORTER_OTLP_HEADERS") != "" ||
		os.Getenv("OTEL_EXPORTER_OTLP_TRACES_HEADERS") != ""
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
	parts := make([]string, 0, len(headers))
	for _, k := range slices.Sorted(maps.Keys(headers)) {
		v := headers[k]
		if k == "" || v == "" {
			continue
		}
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, ",")
}

// applyControlPlaneTracingExporter sets bootstrap-only OTEL_* vars from the job
// payload when the control plane supplied tracing_exporter, the agent is using
// the OpenTelemetry backend, and the agent host has not already configured an
// exporter. Call this after writing BUILDKITE_ENV_FILE so secrets are not
// persisted there.
func applyControlPlaneTracingExporter(setEnv func(name, value string), accepted bool, tracingBackend string, exporter *api.TracingExporter) {
	if !accepted || tracingBackend != tracetools.BackendOpenTelemetry {
		return
	}
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
