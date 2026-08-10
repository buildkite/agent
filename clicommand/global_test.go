package clicommand

import (
	"context"
	"fmt"
	"runtime"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/urfave/cli/v3"
)

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

func TestGitCloneFlagsFlag(t *testing.T) {
	if got, want := GitCloneFlagsFlag.Value, fmt.Sprintf("-v -c checkout.workers=%d", runtime.GOMAXPROCS(0)); got != want {
		t.Errorf("GitCloneFlagsFlag.Value = %q, want %q", got, want)
	}

	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "environment overrides default",
			want: "--no-checkout",
		},
		{
			name: "command line overrides environment",
			args: []string{"--git-clone-flags", "--filter=blob:none"},
			want: "--filter=blob:none",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("BUILDKITE_GIT_CLONE_FLAGS", "--no-checkout")

			flag := &cli.StringFlag{
				Name:    GitCloneFlagsFlag.Name,
				Value:   GitCloneFlagsFlag.Value,
				Sources: cli.EnvVars("BUILDKITE_GIT_CLONE_FLAGS"),
			}

			var got string
			command := &cli.Command{
				Name:  "test",
				Flags: []cli.Flag{flag},
				Action: func(_ context.Context, command *cli.Command) error {
					got = command.String("git-clone-flags")
					return nil
				},
			}

			if err := command.Run(t.Context(), append([]string{"test"}, test.args...)); err != nil {
				t.Fatalf("command.Run() error = %v", err)
			}
			if got != test.want {
				t.Errorf("git-clone-flags = %q, want %q", got, test.want)
			}
		})
	}
}
