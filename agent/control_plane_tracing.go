package agent

import (
	"log/slog"
	"maps"
	"net/url"
	"os"
	"slices"
	"strings"

	"github.com/buildkite/agent/v4/api"
	envutil "github.com/buildkite/agent/v4/env"
	"github.com/buildkite/agent/v4/tracetools"
)

// LocalTracingConfig captures the operator's explicit local tracing choices,
// which always take precedence over control-plane (registration) tracing
// policy. Design: https://linear.app/buildkite/issue/A-1641
type LocalTracingConfig struct {
	// PropagateTraceparentSet is whether the operator explicitly configured
	// tracing-propagate-traceparent (CLI flag, config file, or environment),
	// so its value — even an explicit false — must not be overridden.
	PropagateTraceparentSet bool

	// OTLPDestinationSet is whether the agent's host environment already
	// provides an OTLP destination (endpoint or headers, generic or
	// traces-specific).
	OTLPDestinationSet bool
}

// HasLocalOTLPDestination reports whether the agent's host environment
// already configures an OTLP destination. Presence counts even when the value
// is empty: an operator who set the variable made a choice. Protocol-, cert-
// or timeout-only variables deliberately do not count as a destination.
func HasLocalOTLPDestination() bool {
	for _, name := range envutil.OTLPDestinationVars {
		if _, ok := os.LookupEnv(name); ok {
			return true
		}
	}
	return false
}

// ApplyControlPlaneTracing merges control-plane tracing policy from an agent
// registration response into a single worker's copy of the agent
// configuration. Local configuration always wins: a local non-OTel backend
// disables the whole policy, a local OTLP destination disables the server
// exporter, and an explicitly-set local propagate-traceparent keeps its
// value. Logs one line describing what was applied or ignored; exporter
// endpoints are reduced to scheme+host and headers are never logged.
func ApplyControlPlaneTracing(l *slog.Logger, conf *AgentConfiguration, tracing *api.AgentTracing, local LocalTracingConfig) {
	if tracing == nil {
		return
	}

	// Validate the server policy regardless of local backend: a policy for an
	// unsupported backend is ignored in full, even when the local backend is
	// already OpenTelemetry (its exporter and propagation settings were meant
	// for that other backend, not ours).
	if tracing.Backend != tracetools.BackendOpenTelemetry {
		l.Warn("Ignoring control-plane tracing configuration: unsupported backend", "backend", tracing.Backend)
		return
	}
	conf.OpenTelemetryTracing = true
	// From here on, the effective backend is OpenTelemetry.

	parts := []string{
		"backend=" + tracing.Backend,
	}

	switch {
	case tracing.Exporter == nil || tracing.Exporter.Endpoint == "":
		// No exporter supplied; the backend/propagation policy still applies.
	case local.OTLPDestinationSet:
		parts = append(parts, "exporter=ignored (local OTEL_EXPORTER_OTLP_* destination takes precedence)")
	default:
		conf.ControlPlaneTracingExporter = tracing.Exporter
		parts = append(parts, "exporter="+sanitizedEndpoint(tracing.Exporter.Endpoint))
	}

	l.Info("Control-plane tracing configuration applied", "configuration", strings.Join(parts, ", "))
}

// sanitizedEndpoint reduces an exporter endpoint to scheme://host for
// logging. Paths and queries are omitted because some providers place ingest
// keys there; an unparseable URL is not echoed at all.
func sanitizedEndpoint(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "(unparseable endpoint)"
	}
	return u.Scheme + "://" + u.Host
}

// controlPlaneOTLPEnv builds the environment additions that deliver a
// control-plane exporter to the bootstrap process: the standard
// traces-specific OTel variables. Hooks, plugins, and the job command inherit
// them from bootstrap, so job-level OTel tooling can export spans to the same
// collector as the agent's own trace. Protocol and headers are set only when
// supplied: the OTel spec treats an empty variable as unset, but not every
// SDK does, so absent fields are omitted rather than injected empty.
func controlPlaneOTLPEnv(exporter *api.TracingExporter) map[string]string {
	otlpEnv := map[string]string{
		envutil.OTELTracesEndpoint: exporter.Endpoint,
	}
	if exporter.Protocol != "" {
		otlpEnv[envutil.OTELTracesProtocol] = exporter.Protocol
	}
	if headers := otlpTracesHeaderValue(exporter.Headers); headers != "" {
		otlpEnv[envutil.OTELTracesHeaders] = headers
	}
	return otlpEnv
}

// otlpTracesHeaderValue encodes exporter headers in the comma-separated
// key=value form the OTel SDK env parser expects. Values are path-escaped
// (the SDK path-unescapes them), so commas or percent signs in a credential
// cannot corrupt parsing. Literal '+' is additionally escaped as %2B because
// some SDK parsers (e.g. Java's) use form-style decoding that turns '+' into
// a space; compliant path-unescaping leaves %2B as '+' either way. Keys are
// not escaped: the SDK validates them as HTTP tokens without unescaping.
// Keys are sorted for deterministic output.
func otlpTracesHeaderValue(headers map[string]string) string {
	pairs := make([]string, 0, len(headers))
	for _, k := range slices.Sorted(maps.Keys(headers)) {
		v := strings.ReplaceAll(url.PathEscape(headers[k]), "+", "%2B")
		pairs = append(pairs, k+"="+v)
	}
	return strings.Join(pairs, ",")
}
