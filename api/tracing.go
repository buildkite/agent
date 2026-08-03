package api

// TracingExporter is OTLP exporter configuration supplied by the control plane
// (registration response). It must never be copied into job.env.
type TracingExporter struct {
	Endpoint string            `json:"endpoint"`
	Protocol string            `json:"protocol"`
	Headers  map[string]string `json:"headers"`
}

// AgentRegistrationTracing is tracing policy and optional exporter settings
// supplied by the control plane at registration.
type AgentRegistrationTracing struct {
	Backend              string           `json:"backend"`
	PropagateTraceparent bool             `json:"propagate_traceparent"`
	Exporter             *TracingExporter `json:"exporter,omitempty"`
}
