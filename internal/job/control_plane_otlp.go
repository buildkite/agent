package job

import (
	"encoding/json"
	"os"

	"github.com/buildkite/agent/v3/env"
)

// cleanupControlPlaneOTLPEnv removes the control-plane OTLP exporter
// variables that the job runner injected into this (bootstrap) process's
// environment, and restores any job-env values they displaced, so hooks and
// the job command never see the exporter credential while a pipeline's own
// OTEL_EXPORTER_OTLP_TRACES_* settings still reach the job's tools. It must
// run after tracing and the optional OTLP job logger are initialized (their
// exporters are constructed by then) and before the Job API starts (which
// serves e.shell.Env).
//
// Cleanup is gated on the marker having been authentically set by the job
// runner: the marker is scrubbed from the backend job env before env files
// are written and is protected from within-job sources, so a pipeline cannot
// use it to trick bootstrap into stripping an operator's baked-in OTEL_*
// configuration. Without the marker, nothing is touched.
func (e *Executor) cleanupControlPlaneOTLPEnv() {
	if marker, _ := e.shell.Env.Get(env.ControlPlaneOTLPMarker); marker != "true" {
		return
	}

	restoreJSON, _ := e.shell.Env.Get(env.ControlPlaneOTLPRestore)

	names := make([]string, 0, len(env.OTELTracesVars)+2)
	names = append(names, env.OTELTracesVars...)
	names = append(names, env.ControlPlaneOTLPMarker, env.ControlPlaneOTLPRestore)
	for _, name := range names {
		e.shell.Env.Remove(name)
		_ = os.Unsetenv(name)
	}

	if restoreJSON == "" {
		return
	}
	restore := make(map[string]string)
	if err := json.Unmarshal([]byte(restoreJSON), &restore); err != nil {
		e.shell.Warningf("Couldn't restore OTLP env vars displaced by control-plane exporter configuration: %v", err)
		return
	}
	// Only the three known variables are restorable. The restore var is
	// agent-authoritative, but stay conservative regardless.
	for _, name := range env.OTELTracesVars {
		v, ok := restore[name]
		if !ok {
			continue
		}
		e.shell.Env.Set(name, v)
		_ = os.Setenv(name, v)
	}
}
