package agent

import (
	"maps"
	"net/url"
	"os"
	"slices"
	"strings"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/agent/v3/tracetools"
)

// ControlPlaneOTLPTracingFeature is advertised at registration when this agent
// binary can consume control-plane OTLP exporter config (apply to bootstrap env
// and scrub pipeline-supplied exporter keys from job.env).
const ControlPlaneOTLPTracingFeature = "control-plane-otlp-tracing"

// defaultOTLPProtocol is assumed when registration supplies an exporter without
// naming a protocol. The control plane exports over HTTP with protobuf, so this
// matches it rather than the OTel SDK's gRPC default.
const defaultOTLPProtocol = "http/protobuf"

// ApplyRegistrationTracing merges control-plane tracing policy into the worker's
// runtime config. Explicit local backend and propagation settings win. When
// registration supplies an OpenTelemetry exporter, it is stored for bootstrap
// delivery via process environment variables.
//
// Registration can enable tracing on an agent that configured none, so what was
// accepted is logged: an operator seeing traffic to a collector they did not
// configure should be able to find out why from the agent log alone.
func (c *AgentConfiguration) ApplyRegistrationTracing(
	l logger.Logger,
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
		l.Infof(
			"Buildkite sent OpenTelemetry tracing configuration for this agent. Ignoring it, because the tracing backend is set to %q locally",
			c.TracingBackend,
		)
		return
	}
	if !backendLocallyConfigured {
		l.Infof("Buildkite enabled OpenTelemetry tracing for this agent")
	}

	if !propagateTraceparentLocallyConfigured {
		c.TracingPropagateTraceparent = tracing.PropagateTraceparent
	}

	if tracing.Exporter == nil || tracing.Exporter.Endpoint == "" {
		return
	}
	c.TracingExporter = tracing.Exporter

	if hostHasOTelExporterConfig() {
		l.Infof("Buildkite sent an OTLP trace exporter. Ignoring it, because this host sets OTEL_EXPORTER_OTLP_* itself")
		return
	}

	protocol := tracing.Exporter.Protocol
	if protocol == "" {
		protocol = defaultOTLPProtocol
	}
	headers := "no request headers"
	if len(tracing.Exporter.Headers) > 0 {
		headers = "request headers supplied by Buildkite"
	}
	l.Infof(
		"Exporting job traces to %s over %s, with %s",
		exporterEndpointForLog(tracing.Exporter.Endpoint), protocol, headers,
	)
}

// exporterEndpointForLog reduces an exporter endpoint to its scheme and host.
// Some vendors carry an ingest key in the path or query, so those are left out
// rather than logged.
func exporterEndpointForLog(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return "an unparseable endpoint"
	}
	return u.Scheme + "://" + u.Host
}

// hostHasOTelExporterConfig reports whether the agent process already has a
// local OTLP endpoint or headers. Protocol-only leftovers do not count as a
// full local override. Local operator config takes precedence over
// control-plane injection when an endpoint or headers are set.
func hostHasOTelExporterConfig() bool {
	return os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" ||
		os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") != "" ||
		os.Getenv("OTEL_EXPORTER_OTLP_HEADERS") != "" ||
		os.Getenv("OTEL_EXPORTER_OTLP_TRACES_HEADERS") != ""
}

// controlPlaneExporterApplies reports whether registration exporter config will
// be injected into a job's bootstrap environment.
//
// Everything this feature takes away from a job, namely the job.env scrub and
// removing exporter values before hooks run, is conditional on this. When the
// control plane supplies no exporter there is no organisation credential in the
// job environment to protect, so it is left exactly as it is without this
// feature and anyone configuring OTLP export themselves is unaffected.
func controlPlaneExporterApplies(tracingBackend string, exporter *api.TracingExporter) bool {
	if tracingBackend != tracetools.BackendOpenTelemetry {
		return false
	}
	if exporter == nil || exporter.Endpoint == "" {
		return false
	}
	return !hostHasOTelExporterConfig()
}

// scrubJobOTelExporterEnv removes the agent-owned exporter marker from job.env
// always, and the exporter destination, auth, and TLS keys only when the control
// plane is supplying exporter config for this job. Returns the names removed,
// for BUILDKITE_IGNORED_ENV.
func scrubJobOTelExporterEnv(jobEnv map[string]string, exporterApplies bool) []string {
	scrubbed := env.ScrubOTelExporterMarker(jobEnv)
	if !exporterApplies {
		return scrubbed
	}
	return append(scrubbed, env.ScrubOTelExporterKeys(jobEnv)...)
}

// encodeOTLPHeaders formats a header map as OTEL_EXPORTER_OTLP_HEADERS expects:
// comma-separated key=value pairs with URL-path-escaped values (OTel SDK
// PathUnescapes on read). Keys are sorted for stable output.
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
		parts = append(parts, k+"="+url.PathEscape(v))
	}
	return strings.Join(parts, ",")
}

// applyControlPlaneTracingExporter sets bootstrap OTEL_* vars from registration
// exporter config, plus the marker telling bootstrap to remove them again before
// hooks and the command run. Call only when controlPlaneExporterApplies, and
// after writing BUILDKITE_ENV_FILE so credentials are not persisted there.
//
// Only the traces-specific variables are set. Registration configures a traces
// endpoint, and the OTLP logs and metrics SDKs fall back to the generic
// variables while honouring their own signal-specific endpoint, so generic
// headers would let another signal carry this credential elsewhere.
// Traces-specific values also beat a leftover generic or traces protocol.
//
// Same-UID processes can read these values from the bootstrap environ (the
// same exposure that already applies to BUILDKITE_AGENT_ACCESS_TOKEN).
func applyControlPlaneTracingExporter(setEnv func(name, value string), exporter *api.TracingExporter) {
	protocol := exporter.Protocol
	if protocol == "" {
		protocol = defaultOTLPProtocol
	}
	setEnv("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL", protocol)
	// Full traces URL from the control plane (includes /v1/traces).
	setEnv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", exporter.Endpoint)

	if encoded := encodeOTLPHeaders(exporter.Headers); encoded != "" {
		setEnv("OTEL_EXPORTER_OTLP_TRACES_HEADERS", encoded)
	}

	setEnv(env.OTelExporterMarkerEnv, "true")
}
