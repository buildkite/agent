package job

import (
	"os"
	"testing"

	"github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/internal/shell"
)

// No t.Parallel in these tests: they manipulate the process environment.

func newOTLPCleanupExecutor(t *testing.T) *Executor {
	t.Helper()
	sh := shell.NewTestShell(t)
	sh.Env = env.FromSlice(os.Environ())
	return &Executor{shell: sh}
}

func TestCleanupControlPlaneOTLPEnv(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "https://collector.example/v1/traces")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL", "http/protobuf")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_HEADERS", "Authorization=Bearer%20secret")
	t.Setenv("BUILDKITE_CONTROL_PLANE_OTLP", "true")
	t.Setenv("BUILDKITE_CONTROL_PLANE_OTLP_RESTORE", `{"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT":"https://pipeline.example","OTEL_EXPORTER_OTLP_TRACES_HEADERS":""}`)

	e := newOTLPCleanupExecutor(t)
	e.cleanupControlPlaneOTLPEnv()

	// The injected protocol/headers, marker and restore vars are gone from
	// both the shell env (hooks/commands) and the process env.
	for _, name := range []string{
		"OTEL_EXPORTER_OTLP_TRACES_PROTOCOL",
		"BUILDKITE_CONTROL_PLANE_OTLP",
		"BUILDKITE_CONTROL_PLANE_OTLP_RESTORE",
	} {
		if v, ok := e.shell.Env.Get(name); ok {
			t.Errorf("shell env %s = %q, want removed", name, v)
		}
		if v, ok := os.LookupEnv(name); ok {
			t.Errorf("process env %s = %q, want removed", name, v)
		}
	}

	// Displaced pipeline values are restored exactly, including the
	// explicitly-empty headers var.
	if v, ok := e.shell.Env.Get("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"); !ok || v != "https://pipeline.example" {
		t.Errorf("shell env OTEL_EXPORTER_OTLP_TRACES_ENDPOINT = %q, %t, want restored pipeline value", v, ok)
	}
	if v, ok := os.LookupEnv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"); !ok || v != "https://pipeline.example" {
		t.Errorf("process env OTEL_EXPORTER_OTLP_TRACES_ENDPOINT = %q, %t, want restored pipeline value", v, ok)
	}
	if v, ok := e.shell.Env.Get("OTEL_EXPORTER_OTLP_TRACES_HEADERS"); !ok || v != "" {
		t.Errorf("shell env OTEL_EXPORTER_OTLP_TRACES_HEADERS = %q, %t, want restored empty value", v, ok)
	}
}

func TestCleanupControlPlaneOTLPEnvWithoutMarker(t *testing.T) {
	// Operator-baked configuration with no marker must be untouched.
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "https://baked.example")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_HEADERS", "Authorization=operator")

	e := newOTLPCleanupExecutor(t)
	e.cleanupControlPlaneOTLPEnv()

	if v, ok := e.shell.Env.Get("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"); !ok || v != "https://baked.example" {
		t.Errorf("shell env OTEL_EXPORTER_OTLP_TRACES_ENDPOINT = %q, %t, want untouched", v, ok)
	}
	if v, ok := e.shell.Env.Get("OTEL_EXPORTER_OTLP_TRACES_HEADERS"); !ok || v != "Authorization=operator" {
		t.Errorf("shell env OTEL_EXPORTER_OTLP_TRACES_HEADERS = %q, %t, want untouched", v, ok)
	}
}

func TestCleanupControlPlaneOTLPEnvRequiresExactMarker(t *testing.T) {
	// The marker must parse as exactly "true"; other truthy-looking values
	// don't trigger cleanup.
	t.Setenv("BUILDKITE_CONTROL_PLANE_OTLP", "1")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "https://baked.example")

	e := newOTLPCleanupExecutor(t)
	e.cleanupControlPlaneOTLPEnv()

	if v, ok := e.shell.Env.Get("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"); !ok || v != "https://baked.example" {
		t.Errorf("shell env OTEL_EXPORTER_OTLP_TRACES_ENDPOINT = %q, %t, want untouched", v, ok)
	}
}

func TestCleanupControlPlaneOTLPEnvBadRestoreJSON(t *testing.T) {
	// A corrupt restore snapshot still removes the injected vars; it just
	// can't restore anything.
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "https://collector.example/v1/traces")
	t.Setenv("BUILDKITE_CONTROL_PLANE_OTLP", "true")
	t.Setenv("BUILDKITE_CONTROL_PLANE_OTLP_RESTORE", "{not json")

	e := newOTLPCleanupExecutor(t)
	e.cleanupControlPlaneOTLPEnv()

	for _, name := range []string{
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
		"BUILDKITE_CONTROL_PLANE_OTLP",
		"BUILDKITE_CONTROL_PLANE_OTLP_RESTORE",
	} {
		if v, ok := e.shell.Env.Get(name); ok {
			t.Errorf("shell env %s = %q, want removed", name, v)
		}
	}
}
