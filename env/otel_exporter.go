package env

import "os"

// OTelExporterKeys are standard OTLP exporter environment variables that carry
// destination and auth. They must not come from pipeline job.env, and must be
// stripped from the command/hook shell environment after the TracerProvider has
// been initialized from process env.
var OTelExporterKeys = []string{
	"OTEL_EXPORTER_OTLP_ENDPOINT",
	"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
	"OTEL_EXPORTER_OTLP_HEADERS",
	"OTEL_EXPORTER_OTLP_PROTOCOL",
	"OTEL_EXPORTER_OTLP_TRACES_PROTOCOL",
	"OTEL_EXPORTER_OTLP_TRACES_HEADERS",
}

// StripOTelExporter removes OTLP exporter credentials from environ after the
// TracerProvider has already been initialized from process env. Hooks and
// commands must not see these values.
func StripOTelExporter(environ *Environment) {
	if environ == nil {
		return
	}
	for _, key := range OTelExporterKeys {
		environ.Remove(key)
	}
}

// ClearOTelExporterFromProcess unsets OTLP exporter credentials from the
// current process environment after TracerProvider init so child processes
// cannot read them via /proc/$PPID/environ.
func ClearOTelExporterFromProcess() {
	for _, key := range OTelExporterKeys {
		_ = os.Unsetenv(key)
	}
}
