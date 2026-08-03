package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/api"
	envutil "github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/logger"
	"github.com/google/go-cmp/cmp"
)

// controlPlaneTestRunner builds the minimal JobRunner that createEnvironment
// needs: a job env, an agent configuration, real temp env files, and an API
// client that never makes a request (createEnvironment only reads its
// config).
func controlPlaneTestRunner(t *testing.T, jobEnv map[string]string, agentConf AgentConfiguration) *JobRunner {
	t.Helper()

	envShellFile, err := os.CreateTemp(t.TempDir(), "env-*.sh")
	if err != nil {
		t.Fatalf("os.CreateTemp() error = %v", err)
	}
	envJSONFile, err := os.CreateTemp(t.TempDir(), "env-*.json")
	if err != nil {
		t.Fatalf("os.CreateTemp() error = %v", err)
	}

	return &JobRunner{
		agentLogger:  logger.Discard,
		envShellFile: envShellFile,
		envJSONFile:  envJSONFile,
		apiClient: api.NewClient(logger.Discard, api.Config{
			Endpoint: "http://localhost/fake",
			Token:    "llamas",
		}),
		conf: JobRunnerConfig{
			Job: &api.Job{
				ID:  "job-1",
				Env: jobEnv,
			},
			AgentConfiguration: agentConf,
		},
	}
}

// TestCreateEnvironmentControlPlaneOTLP exercises the real
// JobRunner.createEnvironment boundary for control-plane OTLP delivery: the
// exporter (including its credentialed headers) must reach only the bootstrap
// process env, never the job env files, and spoofed marker/restore values in
// the backend job env must be scrubbed before those files are written.
func TestCreateEnvironmentControlPlaneOTLP(t *testing.T) {
	t.Parallel()

	jobEnv := map[string]string{
		"BUILDKITE_COMMAND": "echo hello",
		// Spoofed reserved names, sent as backend job env. Bootstrap must
		// never see these as authentic.
		envutil.ControlPlaneOTLPMarker:  "true",
		envutil.ControlPlaneOTLPRestore: `{"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT":"https://evil.example"}`,
		// A pipeline-set traces endpoint that collides with the injected one:
		// it must be snapshotted for restore. Protocol is present-but-empty
		// (must be snapshotted as ""), headers are absent (must be omitted).
		envutil.OTELTracesEndpoint: "https://pipeline.example",
		envutil.OTELTracesProtocol: "",
	}

	r := controlPlaneTestRunner(t, jobEnv, AgentConfiguration{
		TracingBackend: "opentelemetry",
		ControlPlaneTracingExporter: &api.TracingExporter{
			Endpoint: "https://collector.example/v1/traces",
			Protocol: "http/protobuf",
			Headers:  map[string]string{"Authorization": testBearerToken},
		},
	})

	got, err := r.createEnvironment(t.Context())
	if err != nil {
		t.Fatalf("createEnvironment() error = %v", err)
	}
	bootstrapEnv := envutil.FromSlice(got)

	// The bootstrap process env carries the exporter, marker, and snapshot.
	wantBootstrap := map[string]string{
		envutil.OTELTracesEndpoint:     "https://collector.example/v1/traces",
		envutil.OTELTracesProtocol:     "http/protobuf",
		envutil.OTELTracesHeaders:      "Authorization=Bearer%20super-secret-token",
		envutil.ControlPlaneOTLPMarker: "true",
	}
	for name, want := range wantBootstrap {
		if v, _ := bootstrapEnv.Get(name); v != want {
			t.Errorf("bootstrap env %s = %q, want %q", name, v, want)
		}
	}

	restoreJSON, _ := bootstrapEnv.Get(envutil.ControlPlaneOTLPRestore)
	var restore map[string]string
	if err := json.Unmarshal([]byte(restoreJSON), &restore); err != nil {
		t.Fatalf("unmarshaling restore snapshot %q: %v", restoreJSON, err)
	}
	wantRestore := map[string]string{
		envutil.OTELTracesEndpoint: "https://pipeline.example", // displaced pipeline value
		envutil.OTELTracesProtocol: "",                         // present-but-empty preserved
		// OTELTracesHeaders absent: omitted so restore leaves it unset
	}
	if diff := cmp.Diff(wantRestore, restore); diff != "" {
		t.Errorf("restore snapshot diff (-want +got):\n%s", diff)
	}

	// The env files hold the job env only: no marker, no restore, no
	// credential, no injected endpoint — but the pipeline's own value stays.
	for _, f := range []*os.File{r.envShellFile, r.envJSONFile} {
		content, err := os.ReadFile(f.Name())
		if err != nil {
			t.Fatalf("os.ReadFile(%q) error = %v", f.Name(), err)
		}
		s := string(content)
		for _, banned := range []string{
			envutil.ControlPlaneOTLPMarker,
			"super-secret-token",
			"collector.example",
			"evil.example",
		} {
			if strings.Contains(s, banned) {
				t.Errorf("env file %s contains %q:\n%s", filepath.Base(f.Name()), banned, s)
			}
		}
		if !strings.Contains(s, "https://pipeline.example") {
			t.Errorf("env file %s is missing the pipeline-set traces endpoint:\n%s", filepath.Base(f.Name()), s)
		}
	}
}

// TestCreateEnvironmentControlPlaneOTLPSpoofWithoutExporter covers the
// higher-stakes spoof: no control-plane exporter is configured (e.g. the
// operator baked their own OTEL_* vars into the image, so the server exporter
// was ignored), and the job env tries to smuggle in the marker and a restore
// snapshot to make bootstrap strip or rewrite those operator values.
func TestCreateEnvironmentControlPlaneOTLPSpoofWithoutExporter(t *testing.T) {
	t.Parallel()

	spoofMarker := envutil.ControlPlaneOTLPMarker
	spoofRestore := envutil.ControlPlaneOTLPRestore
	if runtime.GOOS == "windows" {
		// Case-variant names must be scrubbed too on case-insensitive
		// platforms: bootstrap's env.Environment normalizes on ingest, so a
		// mixed-case survivor would read as authentic.
		spoofMarker = "buildkite_Control_Plane_OTLP"
		spoofRestore = "buildkite_Control_Plane_OTLP_Restore"
	}

	jobEnv := map[string]string{
		"BUILDKITE_COMMAND": "echo hello",
		spoofMarker:         "true",
		spoofRestore:        `{"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT":"https://evil.example"}`,
	}

	r := controlPlaneTestRunner(t, jobEnv, AgentConfiguration{})

	got, err := r.createEnvironment(t.Context())
	if err != nil {
		t.Fatalf("createEnvironment() error = %v", err)
	}
	bootstrapEnv := envutil.FromSlice(got)

	for _, name := range []string{envutil.ControlPlaneOTLPMarker, envutil.ControlPlaneOTLPRestore} {
		if v, ok := bootstrapEnv.Get(name); ok {
			t.Errorf("bootstrap env contains spoofed %s = %q, want scrubbed", name, v)
		}
	}
	for _, f := range []*os.File{r.envShellFile, r.envJSONFile} {
		content, err := os.ReadFile(f.Name())
		if err != nil {
			t.Fatalf("os.ReadFile(%q) error = %v", f.Name(), err)
		}
		if s := string(content); strings.Contains(strings.ToUpper(s), envutil.ControlPlaneOTLPMarker) {
			t.Errorf("env file %s contains a spoofed control-plane marker/restore var:\n%s", filepath.Base(f.Name()), s)
		}
	}
}
