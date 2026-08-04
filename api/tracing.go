package api

// AgentTracing is control-plane tracing policy that may be included in the
// agent registration response when the agent advertises the
// control-plane-otlp-tracing capability. Design:
// https://linear.app/buildkite/issue/A-1641
type AgentTracing struct {
	Backend              string           `json:"backend"`
	PropagateTraceparent bool             `json:"propagate_traceparent"`
	Exporter             *TracingExporter `json:"exporter,omitempty"`
}

// TracingExporter is an OTLP trace exporter destination supplied by the
// control plane. Headers may carry vendor credentials (e.g. an Authorization
// header): raw endpoints and headers must not be logged or written to the
// persisted job-env files, though the exporter is intentionally delivered
// through the bootstrap process environment so hooks and the job command can
// export spans to the same collector.
type TracingExporter struct {
	Endpoint string            `json:"endpoint"`
	Protocol string            `json:"protocol"`
	Headers  map[string]string `json:"headers,omitempty"`
}
