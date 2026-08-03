package env

import "os"

// OTelExporterMarkerEnv records that the agent injected control-plane OTLP
// exporter configuration into a job's bootstrap environment. Bootstrap uses it
// to remove those values before hooks and the command run.
//
// Without the marker nothing is removed, so an operator's own
// OTEL_EXPORTER_OTLP_* configuration reaches hooks and commands exactly as it
// does when this feature is unused.
const OTelExporterMarkerEnv = "BUILDKITE_OTEL_EXPORTER_FROM_CONTROL_PLANE"

// OTelExporterInjectedKeys are the variables the agent sets from registration
// exporter config. Only these are removed from a job environment, and only when
// the marker is present, so operator-supplied configuration survives.
var OTelExporterInjectedKeys = []string{
	"OTEL_EXPORTER_OTLP_TRACES_PROTOCOL",
	"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
	"OTEL_EXPORTER_OTLP_TRACES_HEADERS",
}

// OTelExporterKeys are OTLP exporter destination, auth, and TLS variables that a
// pipeline must not supply while the control plane is delivering exporter
// config: a job must not redirect the export, attach its own auth, or aim the
// exporter at a CA it controls.
//
// They are scrubbed from job.env only in that case. When the control plane
// supplies nothing, a pipeline may configure its own exporter as before.
var OTelExporterKeys = []string{
	"OTEL_EXPORTER_OTLP_ENDPOINT",
	"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
	"OTEL_EXPORTER_OTLP_HEADERS",
	"OTEL_EXPORTER_OTLP_TRACES_HEADERS",
	"OTEL_EXPORTER_OTLP_PROTOCOL",
	"OTEL_EXPORTER_OTLP_TRACES_PROTOCOL",
	"OTEL_EXPORTER_OTLP_INSECURE",
	"OTEL_EXPORTER_OTLP_TRACES_INSECURE",
	"OTEL_EXPORTER_OTLP_CERTIFICATE",
	"OTEL_EXPORTER_OTLP_TRACES_CERTIFICATE",
	"OTEL_EXPORTER_OTLP_CLIENT_CERTIFICATE",
	"OTEL_EXPORTER_OTLP_TRACES_CLIENT_CERTIFICATE",
	"OTEL_EXPORTER_OTLP_CLIENT_KEY",
	"OTEL_EXPORTER_OTLP_TRACES_CLIENT_KEY",
}

// ScrubOTelExporterKeys removes OTLP exporter destination, auth, and TLS
// variables from a job environment map, returning the names that were present so
// the caller can report them as ignored. Call only when the control plane is
// supplying exporter config for the job.
func ScrubOTelExporterKeys(jobEnv map[string]string) []string {
	return scrubKeys(jobEnv, OTelExporterKeys, normalizeKeyName)
}

// ScrubOTelExporterMarker removes the agent-owned exporter marker from a job
// environment map, returning it if it was present. The agent sets the marker
// itself when it injects exporter config, so a supplied one is never wanted.
func ScrubOTelExporterMarker(jobEnv map[string]string) []string {
	return scrubKeys(jobEnv, []string{OTelExporterMarkerEnv}, normalizeKeyName)
}

// scrubKeys deletes any key of m matching names, comparing under normalize.
// Windows environment names are case-insensitive, so an exact-key match there
// could be sidestepped with different casing.
func scrubKeys(m map[string]string, names []string, normalize func(string) string) []string {
	unwanted := make(map[string]struct{}, len(names))
	for _, name := range names {
		unwanted[normalize(name)] = struct{}{}
	}

	var scrubbed []string
	for key := range m {
		if _, ok := unwanted[normalize(key)]; !ok {
			continue
		}
		delete(m, key)
		scrubbed = append(scrubbed, key)
	}
	return scrubbed
}

// StripInjectedOTelExporter removes agent-injected exporter values from environ,
// keeping the organisation's credential out of the environment hooks and the job
// command are given. No job needs it, and a step that dumps its environment
// would otherwise write it to the job log, which is stored and readable by
// anyone with access to the build. The default BUILDKITE_REDACTED_VARS patterns
// do not match these names, so nothing else would catch it.
//
// It is a no-op unless environ carries the marker, which leaves
// operator-supplied configuration untouched.
//
// This bounds accidental disclosure rather than a step that goes looking: see
// ClearInjectedOTelExporterFromProcess for what remains reachable.
func StripInjectedOTelExporter(environ *Environment) {
	if environ == nil {
		return
	}
	if marker, ok := environ.Get(OTelExporterMarkerEnv); !ok || marker != "true" {
		return
	}
	for _, key := range OTelExporterInjectedKeys {
		environ.Remove(key)
	}
	environ.Remove(OTelExporterMarkerEnv)
}

// ClearInjectedOTelExporterFromProcess unsets agent-injected exporter values from
// the current process environment, once the TracerProvider and the optional OTLP
// job logger have read them, so anything started afterwards that does not use the
// executor's own environment still does not inherit them.
//
// This does not hide the values from a same-UID reader, and not merely for a
// window before it runs: on Linux /proc/<pid>/environ reports the environment as
// it was at exec, so unsetting here does not rewrite it, and a job step can
// recover these values from the bootstrap process for as long as it runs. That is
// the same exposure that already applies to BUILDKITE_AGENT_ACCESS_TOKEN, and is
// accepted rather than solved here.
func ClearInjectedOTelExporterFromProcess() {
	if os.Getenv(OTelExporterMarkerEnv) != "true" {
		return
	}
	for _, key := range OTelExporterInjectedKeys {
		_ = os.Unsetenv(key)
	}
	_ = os.Unsetenv(OTelExporterMarkerEnv)
}
