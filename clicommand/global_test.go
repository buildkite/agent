package clicommand

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/buildkite/agent/v4/cliconfig"
	"github.com/buildkite/agent/v4/env"
	"github.com/google/go-cmp/cmp"
	"github.com/urfave/cli/v3"
)

func TestProtectedEnvConfigurationSources(t *testing.T) {
	for _, source := range []string{"default", "flags", "environment", "config"} {
		t.Run(source, func(t *testing.T) {
			for _, name := range []string{"BUILDKITE_PROTECTED_ENVIRONMENT_VARIABLES", "BUILDKITE_PROTECT_DANGEROUS_GIT_ENVIRONMENT_VARIABLES"} {
				t.Setenv(name, "")
				if err := os.Unsetenv(name); err != nil {
					t.Fatal(err)
				}
			}
			args := []string{"start", "--token=test-token", "--build-path=" + t.TempDir()}
			switch source {
			case "flags":
				args = append(args, "--protected-environment-variables=BASH_ENV,LD_*", "--protected-environment-variables=HOME", "--protect-dangerous-git-environment-variables")
			case "environment":
				t.Setenv("BUILDKITE_PROTECTED_ENVIRONMENT_VARIABLES", "BASH_ENV, LD_*,HOME")
				t.Setenv("BUILDKITE_PROTECT_DANGEROUS_GIT_ENVIRONMENT_VARIABLES", "true")
			case "config":
				path := filepath.Join(t.TempDir(), "agent.cfg")
				if err := os.WriteFile(path, []byte("protected-environment-variables=BASH_ENV, LD_*,HOME\nprotect-dangerous-git-environment-variables=true\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				args = append(args, "--config", path)
			}
			// Use fresh flags: cli flags retain parse state between runs.
			cmd := &cli.Command{
				Name: "start",
				Flags: []cli.Flag{
					&cli.StringSliceFlag{Name: ProtectedEnvironmentVariablesFlag.Name, Sources: ProtectedEnvironmentVariablesFlag.Sources},
					&cli.BoolFlag{Name: ProtectDangerousGitEnvironmentVariablesFlag.Name, Sources: ProtectDangerousGitEnvironmentVariablesFlag.Sources},
					&cli.StringFlag{Name: "config"},
					&cli.StringFlag{Name: "token"},
					&cli.StringFlag{Name: "build-path"},
					&cli.StringFlag{Name: "endpoint", Value: "https://agent.invalid"},
				},
			}
			cmd.Action = func(_ context.Context, c *cli.Command) error {
				var cfg AgentStartConfig
				loader := cliconfig.Loader{CLI: c, Config: &cfg}
				if _, err := loader.Load(); err != nil {
					return err
				}
				patterns, err := env.ProtectedEnvPatterns(cfg.ProtectedEnvironmentVariables, cfg.ProtectDangerousGitEnvironmentVariables)
				if err != nil {
					return err
				}
				for _, name := range []string{"BASH_ENV", "LD_PRELOAD", "HOME", "GIT_CONFIG_KEY_6"} {
					if got, want := env.MatchesProtectedEnv(name, patterns), source != "default"; got != want {
						t.Errorf("%s protected = %t, want %t", name, got, want)
					}
				}
				for _, name := range []string{"BASH_ENV_SUFFIX", "GIT_AUTHOR_NAME"} {
					if env.MatchesProtectedEnv(name, patterns) {
						t.Errorf("unexpected protection of %s", name)
					}
				}
				return nil
			}
			if err := cmd.Run(t.Context(), args); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAllFlagEnvs(t *testing.T) {
	// This is testing allFlagEnvs, not EnvDumpCommand, but it will certainly
	// detect any changes to env dump's flag env vars!
	got := slices.Sorted(allFlagEnvs(EnvDumpCommand))
	want := []string{
		"BUILDKITE_AGENT_DEBUG",
		"BUILDKITE_AGENT_ENV_DUMP_FORMAT",
		"BUILDKITE_AGENT_EXPERIMENT",
		"BUILDKITE_AGENT_LOG_LEVEL",
		"BUILDKITE_AGENT_NO_COLOR",
		"BUILDKITE_AGENT_PROFILE",
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("allFlagEnvs(EnvDumpCommand) diff (-got +want):\n%s", diff)
	}
}

func TestGitSkipFetchExistingCommitsConfiguration(t *testing.T) {
	for _, source := range []string{"default", "flag", "environment", "config"} {
		t.Run(source, func(t *testing.T) {
			t.Setenv("BUILDKITE_GIT_SKIP_FETCH_EXISTING_COMMITS", "")
			if err := os.Unsetenv("BUILDKITE_GIT_SKIP_FETCH_EXISTING_COMMITS"); err != nil {
				t.Fatal(err)
			}
			args := []string{"test"}
			switch source {
			case "flag":
				args = append(args, "--git-skip-fetch-existing-commits=false")
			case "environment":
				t.Setenv("BUILDKITE_GIT_SKIP_FETCH_EXISTING_COMMITS", "false")
			case "config":
				path := filepath.Join(t.TempDir(), "agent.cfg")
				if err := os.WriteFile(path, []byte("git-skip-fetch-existing-commits=false\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				args = append(args, "--config", path)
			}
			command := &cli.Command{
				Name: "test",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:    GitSkipFetchExistingCommitsFlag.Name,
						Value:   GitSkipFetchExistingCommitsFlag.Value,
						Sources: GitSkipFetchExistingCommitsFlag.Sources,
					},
					&cli.StringFlag{Name: "config"},
				},
				Action: func(_ context.Context, c *cli.Command) error {
					var cfg struct {
						SkipFetch bool `cli:"git-skip-fetch-existing-commits"`
					}
					loader := cliconfig.Loader{CLI: c, Config: &cfg}
					if _, err := loader.Load(); err != nil {
						return err
					}
					if want := source == "default"; cfg.SkipFetch != want {
						t.Errorf("skip fetch = %t, want %t", cfg.SkipFetch, want)
					}
					return nil
				},
			}
			if err := command.Run(t.Context(), args); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestGitFetchBaseBranchFlag(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    string
		wantErr bool
	}{
		{
			name: "defaults to off",
			want: "off",
		},
		{
			name: "can be set to optimistic",
			args: []string{"--git-fetch-base-branch", "optimistic"},
			want: "optimistic",
		},
		{
			name: "can be set to strict",
			args: []string{"--git-fetch-base-branch", "strict"},
			want: "strict",
		},
		{
			name:    "rejects a boolean",
			args:    []string{"--git-fetch-base-branch", "true"},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			flag := &cli.StringFlag{
				Name:             GitFetchBaseBranchFlag.Name,
				Value:            GitFetchBaseBranchFlag.Value,
				ValidateDefaults: GitFetchBaseBranchFlag.ValidateDefaults,
				Validator:        GitFetchBaseBranchFlag.Validator,
			}

			var got string
			command := &cli.Command{
				Name:  "test",
				Flags: []cli.Flag{flag},
				Action: func(_ context.Context, command *cli.Command) error {
					got = command.String("git-fetch-base-branch")
					return nil
				},
			}

			err := command.Run(t.Context(), append([]string{"test"}, test.args...))
			if test.wantErr {
				if err == nil {
					t.Fatalf("command.Run() error = nil, want an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("command.Run() error = %v", err)
			}
			if got != test.want {
				t.Errorf("git-fetch-base-branch = %q, want %q", got, test.want)
			}
		})
	}
}

func TestGitCommitVerificationFlag(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "defaults to strict",
			want: "strict",
		},
		{
			name: "can be overridden with off",
			args: []string{"--git-commit-verification", "off"},
			want: "off",
		},
		{
			name: "can require successful verification",
			args: []string{"--git-commit-verification", "fail-closed"},
			want: "fail-closed",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			flag := &cli.StringFlag{
				Name:             GitCommitVerificationFlag.Name,
				Value:            GitCommitVerificationFlag.Value,
				ValidateDefaults: GitCommitVerificationFlag.ValidateDefaults,
				Validator:        GitCommitVerificationFlag.Validator,
			}

			var got string
			command := &cli.Command{
				Name:  "test",
				Flags: []cli.Flag{flag},
				Action: func(_ context.Context, command *cli.Command) error {
					got = command.String("git-commit-verification")
					return nil
				},
			}

			if err := command.Run(t.Context(), append([]string{"test"}, test.args...)); err != nil {
				t.Fatalf("command.Run() error = %v", err)
			}
			if got != test.want {
				t.Errorf("git-commit-verification = %q, want %q", got, test.want)
			}
		})
	}
}
