package api

// AgentTracing is control-plane tracing policy that may be included in the
// agent registration response when the agent advertises the
// control-plane-otlp-tracing capability. Design:
// https://linear.app/buildkite/issue/A-1641
type AgentTracing struct {
	Backend              string        `json:"backend"`
	PropagateTraceparent bool          `json:"propagate_traceparent"`
	Exporter             *OTLPExporter `json:"exporter,omitempty"`
}

// AgentOTel is shared registration policy for agents advertising
// control-plane-otlp-job-logs. It supersedes AgentTracing when present.
// JobLogs defaults to false. Exporter.Endpoint is a standard OTLP base URL,
// not a signal-specific URL.
type AgentOTel struct {
	Enabled  bool          `json:"enabled"`
	JobLogs  bool          `json:"job_logs"`
	Exporter *OTLPExporter `json:"exporter,omitempty"`
}

// OTLPExporter is an OTLP exporter destination supplied by the control plane.
// AgentTracing uses a signal-specific endpoint; AgentOTel uses a base URL.
// Headers may carry vendor credentials (e.g. an Authorization
// header): raw endpoints and headers must not be logged or written to the
// persisted job-env files, though the exporter is intentionally delivered
// through the bootstrap process environment so hooks and the job command can
// export telemetry to the same collector.
type OTLPExporter struct {
	Endpoint string            `json:"endpoint"`
	Protocol string            `json:"protocol"`
	Headers  map[string]string `json:"headers,omitempty"`
}
