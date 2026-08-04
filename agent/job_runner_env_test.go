package agent

import (
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/api"
	envutil "github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/logger"
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

func controlPlaneTestAgentConf() AgentConfiguration {
	return AgentConfiguration{
		TracingBackend: "opentelemetry",
		ControlPlaneTracingExporter: &api.TracingExporter{
			Endpoint: "https://collector.example/v1/traces",
			Protocol: "http/protobuf",
			Headers:  map[string]string{"Authorization": testBearerToken},
		},
	}
}

// TestCreateEnvironmentControlPlaneOTLP exercises the real
// JobRunner.createEnvironment boundary for control-plane OTLP delivery: the
// exporter (endpoint, protocol, credentialed headers) must reach the
// bootstrap process env — from which hooks and the job command inherit it, so
// job-level OTel tooling can export spans to the same collector — while
// staying out of the job env files, and a pipeline-chosen OTLP destination
// must make the agent skip injection entirely.
func TestCreateEnvironmentControlPlaneOTLP(t *testing.T) {
	t.Parallel()

	wantInjected := map[string]string{
		envutil.OTELTracesEndpoint: "https://collector.example/v1/traces",
		envutil.OTELTracesProtocol: "http/protobuf",
		envutil.OTELTracesHeaders:  "Authorization=Bearer%20super-secret-token",
	}

	tests := []struct {
		name   string
		jobEnv map[string]string
		// want is the expected effective value of each traces var in the
		// bootstrap env; a "-" value asserts absence.
		want map[string]string
	}{
		{
			name:   "no collision: all three server values are injected",
			jobEnv: map[string]string{"BUILDKITE_COMMAND": "echo hello"},
			want:   wantInjected,
		},
		{
			name: "pipeline traces protocol does not block, injected protocol wins",
			jobEnv: map[string]string{
				envutil.OTELTracesProtocol: "grpc",
			},
			want: wantInjected,
		},
		{
			name: "pipeline logs/metrics destination vars do not block",
			jobEnv: map[string]string{
				"OTEL_EXPORTER_OTLP_LOGS_ENDPOINT":    "https://pipeline-logs.example",
				"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT": "https://pipeline-metrics.example",
				"OTEL_EXPORTER_OTLP_LOGS_HEADERS":     "x=y",
			},
			want: wantInjected,
		},
		{
			name: "pipeline traces endpoint blocks all injection",
			jobEnv: map[string]string{
				envutil.OTELTracesEndpoint: "https://pipeline.example",
			},
			want: map[string]string{
				envutil.OTELTracesEndpoint: "https://pipeline.example",
				envutil.OTELTracesProtocol: "-",
				envutil.OTELTracesHeaders:  "-",
			},
		},
		{
			name: "pipeline traces headers block all injection, even empty",
			jobEnv: map[string]string{
				envutil.OTELTracesHeaders: "",
			},
			want: map[string]string{
				envutil.OTELTracesEndpoint: "-",
				envutil.OTELTracesProtocol: "-",
				envutil.OTELTracesHeaders:  "",
			},
		},
		{
			name: "pipeline generic endpoint blocks all injection",
			jobEnv: map[string]string{
				"OTEL_EXPORTER_OTLP_ENDPOINT": "https://pipeline.example",
			},
			want: map[string]string{
				envutil.OTELTracesEndpoint: "-",
				envutil.OTELTracesProtocol: "-",
				envutil.OTELTracesHeaders:  "-",
			},
		},
		{
			name: "pipeline generic headers block all injection",
			jobEnv: map[string]string{
				"OTEL_EXPORTER_OTLP_HEADERS": "x=y",
			},
			want: map[string]string{
				envutil.OTELTracesEndpoint: "-",
				envutil.OTELTracesProtocol: "-",
				envutil.OTELTracesHeaders:  "-",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			r := controlPlaneTestRunner(t, tc.jobEnv, controlPlaneTestAgentConf())

			got, err := r.createEnvironment(t.Context())
			if err != nil {
				t.Fatalf("createEnvironment() error = %v", err)
			}
			bootstrapEnv := envutil.FromSlice(got)

			for name, want := range tc.want {
				v, ok := bootstrapEnv.Get(name)
				if want == "-" {
					if ok {
						t.Errorf("bootstrap env %s = %q, want unset", name, v)
					}
					continue
				}
				if v != want {
					t.Errorf("bootstrap env %s = %q, want %q", name, v, want)
				}
			}

			// The env files hold the backend job env only: never the
			// injected endpoint or the credential, but pipeline-set values
			// stay.
			for _, f := range []*os.File{r.envShellFile, r.envJSONFile} {
				content, err := os.ReadFile(f.Name())
				if err != nil {
					t.Fatalf("os.ReadFile(%q) error = %v", f.Name(), err)
				}
				s := string(content)
				for _, banned := range []string{"super-secret-token", "collector.example"} {
					if strings.Contains(s, banned) {
						t.Errorf("env file %s contains %q:\n%s", f.Name(), banned, s)
					}
				}
				for name, v := range tc.jobEnv {
					if !strings.Contains(s, name) {
						t.Errorf("env file %s is missing job env var %s=%q:\n%s", f.Name(), name, v, s)
					}
				}
			}
		})
	}
}

// TestCreateEnvironmentControlPlaneOTLPCaseVariantCollision checks that on
// case-insensitive platforms a case-variant pipeline destination var still
// blocks injection: job env keys are normalized before the collision check,
// matching bootstrap's env.Environment normalization on ingest.
func TestCreateEnvironmentControlPlaneOTLPCaseVariantCollision(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != "windows" {
		t.Skip("env keys are case-sensitive on non-Windows platforms")
	}

	r := controlPlaneTestRunner(t, map[string]string{
		"otel_exporter_otlp_traces_endpoint": "https://pipeline.example",
	}, controlPlaneTestAgentConf())

	got, err := r.createEnvironment(t.Context())
	if err != nil {
		t.Fatalf("createEnvironment() error = %v", err)
	}
	bootstrapEnv := envutil.FromSlice(got)

	if v, _ := bootstrapEnv.Get(envutil.OTELTracesEndpoint); v != "https://pipeline.example" {
		t.Errorf("bootstrap env %s = %q, want pipeline value preserved", envutil.OTELTracesEndpoint, v)
	}
	if v, ok := bootstrapEnv.Get(envutil.OTELTracesHeaders); ok {
		t.Errorf("bootstrap env %s = %q, want unset (injection skipped)", envutil.OTELTracesHeaders, v)
	}
}
