package integration

import (
	"context"
	"maps"
	"regexp"
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/agent"
	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/bintest/v3"
)

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

			err := runJob(t, context.Background(), testRunJobConfig{
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
