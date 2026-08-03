package env

// Control-plane OTLP exporter delivery: when agent registration supplies an
// OTLP trace exporter (endpoint, protocol, and possibly credentialed
// headers), the job runner delivers it to the bootstrap process through the
// standard traces-specific OTel SDK variables below, plus two reserved
// BUILDKITE_ variables. Bootstrap consumes them when constructing its
// exporters, then removes them (and restores any displaced job-env values)
// before any hook or the command runs. See
// docs/plans/2026-08-03-control-plane-otlp-exporter.md.
const (
	// ControlPlaneOTLPMarker marks the OTELTracesVars in the bootstrap
	// process environment as injected by the agent from control-plane
	// (registration) exporter configuration, as opposed to operator-baked
	// values, which must never be touched. It is authentic only when set by
	// the job runner: it is scrubbed from the backend job env before env
	// files are written, and protected from within-job sources via
	// protectedEnv.
	ControlPlaneOTLPMarker = "BUILDKITE_CONTROL_PLANE_OTLP"

	// ControlPlaneOTLPRestore carries a JSON object snapshotting the values
	// the OTELTracesVars had before the agent overwrote them (absent names
	// omitted), so bootstrap can restore them for hooks and the job command.
	ControlPlaneOTLPRestore = "BUILDKITE_CONTROL_PLANE_OTLP_RESTORE"

	// OTELTracesEndpoint, OTELTracesProtocol and OTELTracesHeaders are the
	// standard OTel SDK signal-specific exporter variables used for delivery.
	// Traces-specific rather than generic, so the exporter credential cannot
	// be picked up by another signal (e.g. a logs exporter pointed at a
	// pipeline-chosen endpoint) through the generic-variable fallback.
	OTELTracesEndpoint = "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"
	OTELTracesProtocol = "OTEL_EXPORTER_OTLP_TRACES_PROTOCOL"
	OTELTracesHeaders  = "OTEL_EXPORTER_OTLP_TRACES_HEADERS"
)

// OTELTracesVars are the three standard variables a control-plane exporter is
// delivered through, and whose displaced values are snapshotted for restore.
var OTELTracesVars = []string{OTELTracesEndpoint, OTELTracesProtocol, OTELTracesHeaders}
