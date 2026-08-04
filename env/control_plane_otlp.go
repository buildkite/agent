package env

// Control-plane OTLP exporter delivery: when agent registration supplies an
// OTLP trace exporter (endpoint, protocol, and possibly credentialed
// headers), the job runner delivers it to the bootstrap process through the
// standard traces-specific OTel SDK variables below. Hooks, plugins, and the
// job command inherit them, so job-level ("userspace") OTel tooling can join
// the agent's trace via the propagated traceparent and export its spans to
// the same collector — that pass-through is the point of the feature.
// Design: https://linear.app/buildkite/issue/A-1641
const (
	// OTELTracesEndpoint, OTELTracesProtocol and OTELTracesHeaders are the
	// standard OTel SDK signal-specific exporter variables used for delivery.
	// Traces-specific rather than generic, so the exporter credential cannot
	// be picked up by another signal (e.g. a logs exporter pointed at a
	// pipeline-chosen endpoint) through the generic-variable fallback.
	OTELTracesEndpoint = "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"
	OTELTracesProtocol = "OTEL_EXPORTER_OTLP_TRACES_PROTOCOL"
	OTELTracesHeaders  = "OTEL_EXPORTER_OTLP_TRACES_HEADERS"
)

// OTLPDestinationVars are the variables whose presence — even with an empty
// value — counts as an existing OTLP destination choice: whoever set one made
// a decision about where trace data (or its credentials) go. A destination in
// the agent's own environment makes registration ignore the server exporter
// entirely (see agent.HasLocalOTLPDestination); a destination in a job's
// backend env makes the job runner skip injection for that job. Both checks
// are all-or-nothing so a server credential is never sent to a locally- or
// pipeline-chosen endpoint, nor a local credential to the server's endpoint.
// Protocol-, certificate- or timeout-only variables deliberately do not
// count as a destination.
var OTLPDestinationVars = []string{
	"OTEL_EXPORTER_OTLP_ENDPOINT",
	"OTEL_EXPORTER_OTLP_HEADERS",
	OTELTracesEndpoint,
	OTELTracesHeaders,
}
