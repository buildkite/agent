package integration

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/buildkite/agent/v4/agent"
	"github.com/buildkite/agent/v4/api"
)

func TestBootstrapInternalTracing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX shell commands")
	}
	// Bootstrap must use this test's settings, not the surrounding CI job's.
	t.Setenv("BUILDKITE_ARTIFACT_PATHS", "")
	t.Setenv("BUILDKITE_OPENTELEMETRY_TRACING", "false")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name             string
		pty, largeConfig bool
	}{
		{name: "internal only"},
		{name: "internal only with PTY", pty: true},
		{name: "large internal config", largeConfig: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := make(chan struct{}, 1)
			collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				select {
				case requests <- struct{}{}:
				default:
				}
			}))
			defer collector.Close()
			root := t.TempDir()
			checkEnv := `test -z "${BUILDKITE_INTERNAL_TRACING_EXPORTER+x}" || exit 91
! grep -qi '^BUILDKITE_INTERNAL_TRACING_EXPORTER=' "$BUILDKITE_ENV_FILE" || exit 92
! grep -q '"BUILDKITE_INTERNAL_TRACING_EXPORTER":' "$BUILDKITE_ENV_JSON_FILE" || exit 92
`
			if runtime.GOOS == "linux" {
				checkEnv += `tr '\000' '\n' < "/proc/$PPID/environ" | grep -q '^BUILDKITE_JOB_ID=' || exit 93
if tr '\000' '\n' < "/proc/$PPID/environ" | grep '^BUILDKITE_INTERNAL_TRACING_EXPORTER=' | grep -q '"Authorization"'; then
  echo 'internal collector credential remains in bootstrap startup environment'
  exit 94
fi
`
			}
			command := checkEnv + "echo WORKLOAD_RAN"
			hooks := filepath.Join(root, "hooks")
			if err := os.Mkdir(hooks, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(hooks, "pre-command"), []byte("#!/bin/sh\n"+checkEnv+"echo HOOK_RAN\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			conf := agent.AgentConfiguration{
				BootstrapScript: fmt.Sprintf("%q bootstrap --phases command --no-job-api --disable-warnings-for job-api-disabled --cancel-signal SIGTERM", executable),
				BuildPath:       root, HooksPath: hooks, LocalHooksEnabled: true,
				CommandEval: true, Shell: "/bin/sh -e -c",
				GitCommitVerification: "off", GitMirrorCheckoutMode: "reference",
				RunInPty: tc.pty,
				InternalTracingExporter: &api.TracingExporter{
					Endpoint: collector.URL + "/v1/traces", Protocol: "http/protobuf",
					Headers: map[string]string{"Authorization": "Bearer internal-token"},
				},
			}
			if tc.largeConfig {
				conf.InternalTracingExporter.Headers["X-Internal-Context"] = strings.Repeat("x", 128*1024)
			}
			j := &api.Job{
				ID: defaultJobID, ChunksMaxSizeBytes: 1024, Token: "bkaj_job-token",
				InternalTracing: true,
				Env: map[string]string{
					"BUILDKITE_COMMAND": command, "BUILDKITE_JOB_ID": defaultJobID,
					"BUILDKITE_REPO": ".", "BUILDKITE_COMMIT": "HEAD", "BUILDKITE_BRANCH": "main",
					"BUILDKITE_AGENT_NAME": "test-agent", "BUILDKITE_ORGANIZATION_SLUG": "test-org",
					"BUILDKITE_PIPELINE_SLUG": "test-pipeline", "BUILDKITE_PIPELINE_PROVIDER": "custom",
					"BUILDKITE_BUILD_CHECKOUT_PATH":       root,
					"BUILDKITE_INTERNAL_TRACING_EXPORTER": "job-supplied-untrusted-config",
				},
			}
			e := createTestAgentEndpoint()
			server := e.server()
			defer server.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			if err := runJob(t, ctx, testRunJobConfig{job: j, server: server, agentCfg: conf}); err != nil {
				t.Fatal(err)
			}
			if err := ctx.Err(); err != nil {
				t.Fatalf("credential transport delayed job completion: %v", err)
			}
			logs := e.logsFor(t, defaultJobID)
			if finish := e.finishesFor(t, defaultJobID)[0]; finish.ExitStatus != "0" {
				t.Fatalf("job exit = %q, want 0\n%s", finish.ExitStatus, logs)
			}
			if !strings.Contains(logs, "WORKLOAD_RAN") || !strings.Contains(logs, "HOOK_RAN") || strings.Contains(logs, "internal-token") {
				t.Fatalf("unexpected job log: %s", logs)
			}
			if len(requests) != 0 {
				t.Fatal("credential delivery activated internal export")
			}
		})
	}
}

func TestInternalTracingBootstrapFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix pipe transport")
	}
	for _, tc := range []struct {
		name, bootstrap, exit string
	}{
		{name: "start fails", bootstrap: filepath.Join(t.TempDir(), "missing-bootstrap"), exit: "-1"},
		{name: "exits without reading", bootstrap: "/bin/sh -c 'exit 23'", exit: "23"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := createTestAgentEndpoint()
			server := e.server()
			defer server.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			// With no reader, this payload fills the pipe. Finishing the job
			// must close it and release the writer, not wait for delivery.
			conf := agent.AgentConfiguration{
				BootstrapScript: tc.bootstrap,
				BuildPath:       t.TempDir(),
				InternalTracingExporter: &api.TracingExporter{
					Headers: map[string]string{"Authorization": "internal-token" + strings.Repeat("x", 128*1024)},
				},
			}
			j := &api.Job{
				ID: defaultJobID, ChunksMaxSizeBytes: 1024, Token: "bkaj_job-token",
				InternalTracing: true,
			}
			if err := runJob(t, ctx, testRunJobConfig{job: j, server: server, agentCfg: conf}); err != nil {
				t.Fatal(err)
			}
			if err := ctx.Err(); err != nil {
				t.Fatalf("bootstrap failure delayed job completion: %v", err)
			}
			finishes := e.finishesFor(t, defaultJobID)
			if len(finishes) != 1 || finishes[0].ExitStatus != tc.exit {
				t.Fatalf("job finishes = %+v, want one finish with exit %q", finishes, tc.exit)
			}
			if logs := e.logsFor(t, defaultJobID); strings.Contains(logs, "internal-token") {
				t.Fatalf("job log contains internal credentials: %s", logs)
			}
		})
	}
}
