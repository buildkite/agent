package integration

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/buildkite/agent/v4/agent"
	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/env"
	"github.com/buildkite/bintest/v3"
)

func TestRepositoryAllowlistRejectsCaseVariantWildcard(t *testing.T) {
	t.Parallel()

	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	hooksDir := t.TempDir()
	hookPath := filepath.Join(hooksDir, "pre-checkout")
	const disallowedRepository = "https://disallowed.example/repo.git"
	hookBody := "#!/bin/sh\nexport BUILDKITE_REPO=" + disallowedRepository + "\n"
	if runtime.GOOS == "windows" {
		hookPath += ".bat"
		hookBody = "@echo off\r\nset BUILDKITE_REPO=" + disallowedRepository + "\r\n"
	}
	writeHook := exec.Command(executable, "write-exec", hookPath)
	writeHook.Stdin = strings.NewReader(hookBody)
	if err := writeHook.Run(); err != nil {
		t.Fatal(err)
	}

	git, err := bintest.NewMock(filepath.Join(t.TempDir(), "git"))
	if err != nil {
		t.Fatal(err)
	}
	git.Expect().NotCalled()
	t.Cleanup(func() { git.CheckAndClose(t) }) //nolint:errcheck // bintest logs to t

	e := createTestAgentEndpoint()
	server := e.server()
	t.Cleanup(server.Close)
	j := &api.Job{
		ID:                 defaultJobID,
		ChunksMaxSizeBytes: 1024,
		Token:              "bkaj_job-token",
		Env: map[string]string{
			"BUILDKITE_JOB_ID":               defaultJobID,
			"BUILDKITE_AGENT_NAME":           "test-agent",
			"BUILDKITE_REPO":                 "https://github.com/buildkite/agent",
			"BUILDKITE_COMMIT":               "HEAD",
			"BUILDKITE_BRANCH":               "main",
			"BUILDKITE_ORGANIZATION_SLUG":    "test",
			"BUILDKITE_PIPELINE_SLUG":        "test",
			"BUILDKITE_PIPELINE_PROVIDER":    "custom",
			"buildkite_allowed_repositories": ".*",
			"PATH":                           filepath.Dir(git.Path) + string(os.PathListSeparator) + os.Getenv("PATH"),
		},
	}
	if err := runJob(t, t.Context(), testRunJobConfig{
		job:    j,
		server: server,
		agentCfg: agent.AgentConfiguration{
			BootstrapScript:       fmt.Sprintf("%q bootstrap --phases checkout --no-job-api --cancel-signal SIGTERM", executable),
			BuildPath:             t.TempDir(),
			HooksPath:             hooksDir,
			GitCommitVerification: "strict",
			GitMirrorCheckoutMode: "reference",
			CommandEval:           true,
			CheckoutOverrideMode:  env.CheckoutOverrideFromJob,
			CheckoutAttempts:      1,
			AllowedRepositories:   []*regexp.Regexp{regexp.MustCompile(`^https://github\.com/buildkite/.*$`)},
		},
	}); err != nil {
		t.Fatal(err)
	}

	logs := e.logsFor(t, j.ID)
	if got := e.finishesFor(t, j.ID)[0].ExitStatus; got != "1" {
		t.Errorf("job.ExitStatus = %q, want 1\n%s", got, logs)
	}
	wantRejection := fmt.Sprintf("repository %q is not permitted by --allowed-repositories", disallowedRepository)
	if !strings.Contains(logs, wantRejection) {
		t.Errorf("missing checkout rejection %q:\n%s", wantRejection, logs)
	}
}

func TestConfigAllowlisting(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name                     string
		extraEnv                 map[string]string
		mockBootstrapExpectation func(*bintest.Mock)
		agentConfig              agent.AgentConfiguration
		wantExitStatus           string
		wantSignalReason         string
		wantLogsContain          []string
	}

	tests := []testCase{
		{
			name:     "when allowlisting environment variables, the job is refused if any of the environment variables don't match the configured allowlist",
			extraEnv: map[string]string{"BASH_ENV": "echo crimes"},
			agentConfig: agent.AgentConfiguration{
				AllowedEnvironmentVariables: []*regexp.Regexp{
					regexp.MustCompile("^BUILDKITE.*$"),
				},
			},
			mockBootstrapExpectation: func(bm *bintest.Mock) { bm.Expect().NotCalled() },
			wantExitStatus:           "-1",
			wantLogsContain:          []string{"failed to validate environment variables: BASH_ENV has no match in [^BUILDKITE.*$]"},
			wantSignalReason:         agent.SignalReasonAgentRefused,
		},
		{
			name: "when allowlisting environment variables, the job is accepted if all of the environment variables match the configured allowlist",
			extraEnv: map[string]string{
				"BUILDKITE":               "true",
				"MY_APP_SPECIFIC_ENV_VAR": "tesselate",
			},
			mockBootstrapExpectation: func(bm *bintest.Mock) { bm.Expect().Once().AndExitWith(0) },
			agentConfig: agent.AgentConfiguration{
				AllowedEnvironmentVariables: []*regexp.Regexp{
					regexp.MustCompile("^BUILDKITE.*$"),
					regexp.MustCompile("^MY_APP_.*$"),
				},
			},
			wantExitStatus: "0",
		},
		{
			name:     "when allowlisting repos, the job is refused if the repo doesn't match the configured allowlist",
			extraEnv: map[string]string{"BUILDKITE_REPO": "https://github.com/crimes/cryptohaxx.exe"},
			agentConfig: agent.AgentConfiguration{
				AllowedRepositories: []*regexp.Regexp{
					regexp.MustCompile("^.*github.com/buildkite/agent$"),
				},
			},
			mockBootstrapExpectation: func(bm *bintest.Mock) { bm.Expect().NotCalled() },
			wantExitStatus:           "-1",
			wantLogsContain:          []string{"failed to validate repo: https://github.com/crimes/cryptohaxx.exe has no match in [^.*github.com/buildkite/agent$]"},
			wantSignalReason:         agent.SignalReasonAgentRefused,
		},
		{
			name:     "when allowlisting repos, the job is accepted if the repo matches the configured allowlist",
			extraEnv: map[string]string{"BUILDKITE_REPO": "https://github.com/buildkite/agent"},
			agentConfig: agent.AgentConfiguration{
				AllowedRepositories: []*regexp.Regexp{
					regexp.MustCompile("^https://github.com/buildkite/.*$"),
				},
			},
			mockBootstrapExpectation: func(bm *bintest.Mock) { bm.Expect().Once().AndExitWith(0) },
			wantExitStatus:           "0",
		},
		{
			name: "when the remote mirror does not match the repo allowlist, the mirror is dropped without refusing the job",
			extraEnv: map[string]string{
				"BUILDKITE_REPO":                  "https://github.com/buildkite/agent",
				"BUILDKITE_GIT_REMOTE_MIRROR_URL": "https://mirror.example/buildkite/agent",
			},
			agentConfig: agent.AgentConfiguration{
				AllowedRepositories: []*regexp.Regexp{
					regexp.MustCompile("^https://github.com/buildkite/.*$"),
				},
			},
			mockBootstrapExpectation: func(bm *bintest.Mock) {
				bm.Expect().Once().AndExitWith(0).AndCallFunc(func(c *bintest.Call) {
					if got := c.GetEnv("BUILDKITE_GIT_REMOTE_MIRROR_URL"); got != "" {
						t.Errorf("BUILDKITE_GIT_REMOTE_MIRROR_URL = %q, want dropped", got)
					}
					c.Exit(0)
				})
			},
			wantExitStatus:  "0",
			wantLogsContain: []string{"Remote Git mirror is not permitted by --allowed-repositories; using canonical repository"},
		},
		{
			name: "when both canonical and remote mirror match the repo allowlist, the mirror reaches bootstrap",
			extraEnv: map[string]string{
				"BUILDKITE_REPO":                  "https://github.com/buildkite/agent",
				"BUILDKITE_GIT_REMOTE_MIRROR_URL": "https://mirror.example/buildkite/agent",
			},
			agentConfig: agent.AgentConfiguration{
				AllowedRepositories: []*regexp.Regexp{
					regexp.MustCompile(`^https://(github\.com|mirror\.example)/buildkite/.*$`),
				},
			},
			mockBootstrapExpectation: func(bm *bintest.Mock) {
				bm.Expect().Once().AndExitWith(0).AndCallFunc(func(c *bintest.Call) {
					if got, want := c.GetEnv("BUILDKITE_GIT_REMOTE_MIRROR_URL"), "https://mirror.example/buildkite/agent"; got != want {
						t.Errorf("BUILDKITE_GIT_REMOTE_MIRROR_URL = %q, want %q", got, want)
					}
					c.Exit(0)
				})
			},
			wantExitStatus: "0",
		},
		{
			name:     "when allowlisting plugins, if the plugin source doesn't match the configured allowlist, the job is refused",
			extraEnv: map[string]string{"BUILDKITE_PLUGINS": `[{"github.com/crime-org/super-nasty-plugin#1.0.0":{"some":"config"}}]`},
			agentConfig: agent.AgentConfiguration{
				PluginsEnabled: true,
				AllowedPlugins: []*regexp.Regexp{
					regexp.MustCompile("^github.com/buildkite-plugins/.*$"),
				},
			},
			mockBootstrapExpectation: func(bm *bintest.Mock) { bm.Expect().NotCalled() },
			wantExitStatus:           "-1",
			wantLogsContain:          []string{"failed to validate plugins: github.com/crime-org/super-nasty-plugin#1.0.0 has no match in [^github.com/buildkite-plugins/.*$]"},
			wantSignalReason:         agent.SignalReasonAgentRefused,
		},
		{
			name:     "when allowlisting plugins, if the plugin source matches the configured allowlist, the job is accepted",
			extraEnv: map[string]string{"BUILDKITE_PLUGINS": `[{"github.com/buildkite-plugins/docker#v5.9.2":{"some":"config"}}]`},
			agentConfig: agent.AgentConfiguration{
				PluginsEnabled: true,
				AllowedPlugins: []*regexp.Regexp{
					regexp.MustCompile("^github.com/buildkite-plugins/.*$"),
				},
			},
			mockBootstrapExpectation: func(bm *bintest.Mock) { bm.Expect().Once().AndExitWith(0) },
			wantExitStatus:           "0",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			jobID := defaultJobID

			job := &api.Job{
				ChunksMaxSizeBytes: 1024,
				ID:                 jobID,
				Env: map[string]string{
					"BUILDKITE":         "true",
					"BUILDKITE_COMMAND": "echo hello",
				},
				Token: "bkaj_job-token",
			}

			maps.Copy(job.Env, tc.extraEnv)

			e := createTestAgentEndpoint()
			server := e.server()
			defer server.Close()

			mb := mockBootstrap(t)
			tc.mockBootstrapExpectation(mb)
			defer mb.CheckAndClose(t) //nolint:errcheck // bintest logs to t

			err := runJob(t, t.Context(), testRunJobConfig{
				job:           job,
				server:        server,
				agentCfg:      tc.agentConfig,
				mockBootstrap: mb,
			})
			if err != nil {
				t.Fatalf("runJob() error = %v", err)
			}

			finishedJob := e.finishesFor(t, jobID)[0]

			if got, want := finishedJob.ExitStatus, tc.wantExitStatus; got != want {
				t.Errorf("job.ExitStatus = %q, want %q", got, want)
			}

			logs := e.logsFor(t, jobID)

			for _, want := range tc.wantLogsContain {
				if !strings.Contains(logs, want) {
					t.Errorf("logs = %q, want to contain %q", logs, want)
				}
			}

			if got, want := finishedJob.SignalReason, tc.wantSignalReason; got != want {
				t.Errorf("job.SignalReason = %q, want %q", got, want)
			}
		})
	}
}

func TestRemoteMirrorAllowlistMasksAmbientEnvironment(t *testing.T) {
	tests := []struct {
		name         string
		jobMirrorURL string
	}{
		{name: "backend mirror absent"},
		{name: "backend mirror disallowed", jobMirrorURL: "https://disallowed.example/buildkite/agent"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("BUILDKITE_GIT_REMOTE_MIRROR_URL", "https://ambient.example/buildkite/agent")

			jobID := defaultJobID
			job := &api.Job{
				ChunksMaxSizeBytes: 1024,
				ID:                 jobID,
				Env: map[string]string{
					"BUILDKITE":         "true",
					"BUILDKITE_COMMAND": "echo hello",
					"BUILDKITE_REPO":    "https://github.com/buildkite/agent",
				},
				Token: "bkaj_job-token",
			}
			if tc.jobMirrorURL != "" {
				job.Env["BUILDKITE_GIT_REMOTE_MIRROR_URL"] = tc.jobMirrorURL
			}

			e := createTestAgentEndpoint()
			server := e.server()
			defer server.Close()

			mb := mockBootstrap(t)
			mb.Expect().Once().AndExitWith(0).AndCallFunc(func(c *bintest.Call) {
				if got := c.GetEnv("BUILDKITE_GIT_REMOTE_MIRROR_URL"); got != "" {
					t.Errorf("BUILDKITE_GIT_REMOTE_MIRROR_URL = %q, want ambient value masked", got)
				}
				c.Exit(0)
			})
			defer mb.CheckAndClose(t) //nolint:errcheck // bintest logs to t

			err := runJob(t, t.Context(), testRunJobConfig{
				job:    job,
				server: server,
				agentCfg: agent.AgentConfiguration{
					AllowedRepositories: []*regexp.Regexp{
						regexp.MustCompile("^https://github.com/buildkite/.*$"),
					},
				},
				mockBootstrap: mb,
			})
			if err != nil {
				t.Fatalf("runJob() error = %v", err)
			}
		})
	}
}
