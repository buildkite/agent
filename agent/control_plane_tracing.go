package agent

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/url"
	"os"
	"slices"
	"strings"

	"github.com/buildkite/agent/v3/api"
	envutil "github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/agent/v3/tracetools"
)

// LocalTracingConfig captures the operator's explicit local tracing choices,
// which always take precedence over control-plane (registration) tracing
// policy. See docs/plans/2026-08-03-control-plane-otlp-exporter.md.
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
	for _, name := range []string{
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_EXPORTER_OTLP_HEADERS",
		envutil.OTELTracesEndpoint,
		envutil.OTELTracesHeaders,
	} {
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
func ApplyControlPlaneTracing(l logger.Logger, conf *AgentConfiguration, tracing *api.AgentTracing, local LocalTracingConfig) {
	if tracing == nil {
		return
	}

	if conf.TracingBackend != "" && conf.TracingBackend != tracetools.BackendOpenTelemetry {
		l.Infof("Ignoring control-plane tracing configuration: local tracing-backend %q takes precedence", conf.TracingBackend)
		return
	}

	if conf.TracingBackend == "" {
		if tracing.Backend != tracetools.BackendOpenTelemetry {
			l.Warnf("Ignoring control-plane tracing configuration: unsupported backend %q", tracing.Backend)
			return
		}
		conf.TracingBackend = tracing.Backend
	}
	// From here on, the effective backend is OpenTelemetry.

	if tracing.PropagateTraceparent && !local.PropagateTraceparentSet {
		conf.TracingPropagateTraceparent = true
	}

	parts := []string{
		"backend=" + conf.TracingBackend,
		fmt.Sprintf("propagate_traceparent=%t", conf.TracingPropagateTraceparent),
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

	l.Infof("Control-plane tracing configuration applied: %s", strings.Join(parts, ", "))
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

// controlPlaneOTLPEnv builds the bootstrap-process-only environment additions
// that deliver a control-plane exporter: the three standard traces-specific
// OTel variables, the authenticity marker, and a restore snapshot. effective
// reports the value each variable would otherwise have in the bootstrap
// process environment (job env over agent process env), so bootstrap can
// restore its exact present/empty/absent state for hooks and the command
// after its exporters are constructed.
func controlPlaneOTLPEnv(exporter *api.TracingExporter, effective func(name string) (string, bool)) (map[string]string, error) {
	restore := make(map[string]string)
	for _, name := range envutil.OTELTracesVars {
		if v, ok := effective(name); ok {
			restore[name] = v
		}
	}
	restoreJSON, err := json.Marshal(restore)
	if err != nil {
		return nil, fmt.Errorf("marshaling restore snapshot: %w", err)
	}

	return map[string]string{
		envutil.OTELTracesEndpoint:      exporter.Endpoint,
		envutil.OTELTracesProtocol:      exporter.Protocol,
		envutil.OTELTracesHeaders:       otlpTracesHeaderValue(exporter.Headers),
		envutil.ControlPlaneOTLPMarker:  "true",
		envutil.ControlPlaneOTLPRestore: string(restoreJSON),
	}, nil
}

// otlpTracesHeaderValue encodes exporter headers in the comma-separated
// key=value form the OTel SDK env parser expects. Values are path-escaped
// (the SDK path-unescapes them), so commas or percent signs in a credential
// cannot corrupt parsing. Keys are not escaped: the SDK validates them as
// HTTP tokens without unescaping. Keys are sorted for deterministic output.
func otlpTracesHeaderValue(headers map[string]string) string {
	pairs := make([]string, 0, len(headers))
	for _, k := range slices.Sorted(maps.Keys(headers)) {
		pairs = append(pairs, k+"="+url.PathEscape(headers[k]))
	}
	return strings.Join(pairs, ",")
}
