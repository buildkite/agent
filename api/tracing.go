package api

// AgentTracing is control-plane tracing policy that may be included in the
// agent registration response when the agent advertises the
// control-plane-otlp-tracing capability. See
// docs/plans/2026-08-03-control-plane-otlp-exporter.md.
type AgentTracing struct {
	Backend              string           `json:"backend"`
	PropagateTraceparent bool             `json:"propagate_traceparent"`
	Exporter             *TracingExporter `json:"exporter,omitempty"`
}

// TracingExporter is an OTLP trace exporter destination supplied by the
// control plane. Headers may carry vendor credentials (e.g. an Authorization
// header), so values of this type must be kept out of job environments and
// never logged.
type TracingExporter struct {
	Endpoint string            `json:"endpoint"`
	Protocol string            `json:"protocol"`
	Headers  map[string]string `json:"headers,omitempty"`
}
