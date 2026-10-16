package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/buildkite/agent/v4/api"
	envutil "github.com/buildkite/agent/v4/env"
	"github.com/buildkite/agent/v4/logger"
	"github.com/google/go-cmp/cmp"
)

func TestCreateEnvironmentProtectedEnv(t *testing.T) {
	t.Parallel()

	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			t.Parallel()
			jobEnv := map[string]string{
				"BUILDKITE_PROTECT_DANGEROUS_GIT_ENVIRONMENT_VARIABLES": fmt.Sprint(!enabled),
				"buildkite_protect_dangerous_git_environment_variables": fmt.Sprint(!enabled),
				"BUILDKITE_PROTECTED_ENVIRONMENT_VARIABLES":             "UNRELATED",
				"buildkite_protected_environment_variables":             "UNRELATED",
				"GIT_AUTHOR_NAME":     "job-author",
				"GIT_TERMINAL_PROMPT": "0",
				"GIT_LFS_SKIP_SMUDGE": "1",
			}
			blocked := []string{
				"GIT_CONFIG", "GIT_CONFIG_COUNT", "GIT_CONFIG_KEY_12", "GIT_CONFIG_VALUE_12",
				"GIT_CONFIG_PARAMETERS", "GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM", "GIT_CONFIG_NOSYSTEM",
				"GIT_ALLOW_PROTOCOL",
				"GIT_SSH", "GIT_SSH_COMMAND", "GIT_ASKPASS", "SSH_ASKPASS", "GIT_EXEC_PATH",
				"GIT_EXTERNAL_DIFF", "GIT_PROXY_COMMAND", "GIT_TEMPLATE_DIR", "GIT_PAGER", "PAGER",
				"GIT_EDITOR", "GIT_SEQUENCE_EDITOR", "EDITOR", "VISUAL",
			}
			if runtime.GOOS == "windows" {
				// Exercise filtering of Windows case variants too.
				blocked = append(blocked, "git_config_value_23", "git_ssh_command")
			}
			for _, name := range blocked {
				jobEnv[name] = "job-supplied-value"
			}
			customNames := []string{"BASH_ENV", "LD_PRELOAD", "PATH", "HOME"}
			for _, name := range customNames {
				jobEnv[name] = "custom-job-value"
			}
			r := controlPlaneTestRunner(t, jobEnv, AgentConfiguration{
				ProtectedEnvironmentVariables:           []string{"BASH_ENV", "LD_*", "PATH", "HOME"},
				ProtectDangerousGitEnvironmentVariables: enabled,
			})
			got, err := r.createEnvironment(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			bootstrapEnv := envutil.FromSlice(got)
			if got := bootstrapEnv.GetString("BUILDKITE_PROTECT_DANGEROUS_GIT_ENVIRONMENT_VARIABLES", ""); got != fmt.Sprint(enabled) {
				t.Fatalf("bootstrap policy = %q, want %t", got, enabled)
			}
			if got := bootstrapEnv.GetString("BUILDKITE_PROTECTED_ENVIRONMENT_VARIABLES", ""); got != "BASH_ENV,LD_*,PATH,HOME" {
				t.Fatalf("bootstrap custom policy = %q", got)
			}
			ignored := strings.Split(bootstrapEnv.GetString("BUILDKITE_IGNORED_ENV", ""), ",")
			for _, name := range customNames {
				if bootstrapEnv.Exists(name) || !slices.Contains(ignored, name) {
					t.Errorf("custom protection did not ignore %s", name)
				}
			}
			for _, name := range blocked {
				if bootstrapEnv.Exists(name) == enabled {
					t.Errorf("bootstrap contains %s = %t, want %t", name, bootstrapEnv.Exists(name), !enabled)
				}
				if slices.Contains(ignored, name) != enabled {
					t.Errorf("ignored names = %v, want %s ignored = %t", ignored, name, enabled)
				}
			}
			for _, name := range []string{"GIT_AUTHOR_NAME", "GIT_TERMINAL_PROMPT", "GIT_LFS_SKIP_SMUDGE"} {
				if got := bootstrapEnv.GetString(name, ""); got != jobEnv[name] {
					t.Errorf("%s = %q, want %q", name, got, jobEnv[name])
				}
			}
			for _, path := range []string{r.envShellFile.Name(), r.envJSONFile.Name()} {
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(data), "job-supplied-value") == enabled {
					t.Errorf("env file %s contains filtered values = %t, want %t", path, enabled, !enabled)
				}
				for _, unwanted := range []string{"custom-job-value", "UNRELATED", "buildkite_protect"} {
					if strings.Contains(string(data), unwanted) {
						t.Errorf("env file %s contains untrusted data %q", path, unwanted)
					}
				}
			}
			if jobEnv["GIT_SSH_COMMAND"] != "job-supplied-value" {
				t.Fatal("filtering mutated the original job environment")
			}
		})
	}
}

func TestCreateEnvironmentRepositoryAllowlist(t *testing.T) {
	t.Setenv("BUILDKITE_ALLOWED_REPOSITORIES", "ambient-policy")

	for _, patterns := range [][]string{nil, {`^https://example\.com/repo[0-9]+\.git$`, `^git@example\.com:org/.*$`}} {
		t.Run(fmt.Sprint(patterns), func(t *testing.T) {
			// Protection must strip job values, never the agent's own policy.
			conf := AgentConfiguration{ProtectedEnvironmentVariables: []string{"BUILDKITE_ALLOWED_REPOSITORIES"}}
			for _, pattern := range patterns {
				conf.AllowedRepositories = append(conf.AllowedRepositories, regexp.MustCompile(pattern))
			}
			r := controlPlaneTestRunner(t, map[string]string{
				"BUILDKITE_ALLOWED_REPOSITORIES": "job-policy",
				"buildkite_allowed_repositories": ".*",
				"Buildkite_Allowed_Repositories": "job-policy-mixedcase",
			}, conf)
			got, err := r.createEnvironment(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			value := envutil.FromSlice(got).GetString("BUILDKITE_ALLOWED_REPOSITORIES", "")
			if diff := cmp.Diff(strings.Join(patterns, ","), value); diff != "" {
				t.Errorf("bootstrap policy (-want +got):\n%s", diff)
			}
			var policyEntries []string
			for name, value := range envutil.SeqSlice(got) {
				if strings.EqualFold(name, "BUILDKITE_ALLOWED_REPOSITORIES") {
					policyEntries = append(policyEntries, name+"="+value)
				}
			}
			wantPolicyEntries := []string{"BUILDKITE_ALLOWED_REPOSITORIES=" + strings.Join(patterns, ",")}
			if diff := cmp.Diff(wantPolicyEntries, policyEntries); diff != "" {
				t.Errorf("case-insensitive policy entries (-want +got):\n%s", diff)
			}
			// Container bootstraps must receive the same trusted policy.
			for _, path := range []string{r.envShellFile.Name(), r.envJSONFile.Name()} {
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				for _, untrusted := range []string{"job-policy", "ambient-policy", "buildkite_allowed_repositories"} {
					if strings.Contains(string(data), untrusted) {
						t.Errorf("env file contains an untrusted policy %q: %s", untrusted, data)
					}
				}
				if !strings.Contains(string(data), "BUILDKITE_ALLOWED_REPOSITORIES") {
					t.Errorf("env file is missing the policy: %s", data)
				}
			}
		})
	}
}

func TestCreateEnvironmentShellFormatPreservesTrailingBackslash(t *testing.T) {
	t.Parallel()

	jobEnv := map[string]string{
		"BASE_DIR":    `C:\Program Files (x86)\CODE\`,
		"MULTI_SLASH": `ends with two\\`,
		"QUOTED":      "say \"hi\"\nC:\\path",
		"PLAIN":       "ordinary",
		"NEXT":        "kept-separate",
	}
	r := controlPlaneTestRunner(t, jobEnv, AgentConfiguration{})
	if _, err := r.createEnvironment(t.Context()); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(r.envShellFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for line := range strings.SplitSeq(string(data), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		// A backslash immediately before the closing quote escapes it, so the
		// value runs into the next entry.
		if strings.HasSuffix(value, `\"`) {
			t.Errorf("%s escapes its closing quote: %s", key, line)
		}
		unquoted, err := strconv.Unquote(value)
		if err != nil {
			t.Errorf("strconv.Unquote(%q) error = %v", value, err)
			continue
		}
		got[key] = unquoted
	}
	for key, want := range jobEnv {
		if got[key] != want {
			t.Errorf("shell %s = %q, want %q", key, got[key], want)
		}
	}
	if strings.Contains(got["BASE_DIR"], "kept-separate") || strings.Contains(got["MULTI_SLASH"], "kept-separate") {
		t.Errorf("trailing backslash merged NEXT into another value: %#v", got)
	}

	jsonData, err := os.ReadFile(r.envJSONFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	var jsonEnv map[string]string
	if err := json.Unmarshal(jsonData, &jsonEnv); err != nil {
		t.Fatal(err)
	}
	for key, want := range jobEnv {
		if jsonEnv[key] != want {
			t.Errorf("json %s = %q, want %q", key, jsonEnv[key], want)
		}
	}
}

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
		OpenTelemetryTracing: true,
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

func TestWindowsJobEnvAliasesCannotOverrideAgentControls(t *testing.T) {
	t.Parallel()

	jobEnv, err := normalizeJobEnv(map[string]string{
		"buildkite_agent_endpoint": "https://attacker.invalid",
		"buildkite_command_eval":   "true",
		"buildkite_agent_token":    "synthetic-registration-token",
	}, "windows")
	if err != nil {
		t.Fatal(err)
	}
	r := controlPlaneTestRunner(t, jobEnv, AgentConfiguration{CommandEval: false})
	got, err := r.createEnvironment(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	bootstrapEnv := envutil.FromSlice(got)
	if value, _ := bootstrapEnv.Get("BUILDKITE_AGENT_ENDPOINT"); value != "http://localhost/fake" {
		t.Errorf("endpoint = %q, want agent endpoint", value)
	}
	if value, _ := bootstrapEnv.Get("BUILDKITE_COMMAND_EVAL"); value != "false" {
		t.Errorf("command eval = %q, want false", value)
	}
	if value, exists := bootstrapEnv.Get("BUILDKITE_AGENT_TOKEN"); exists {
		t.Errorf("registration token reached bootstrap: %q", value)
	}
}
