package clicommand

import (
	"path/filepath"
	"testing"

	"github.com/buildkite/agent/v3/env"
)

func TestRebaseJobContextPaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		environ    []string
		contextDir string
		want       map[string]string
	}{
		{
			name: "differing_mount_paths",
			environ: []string{
				"BUILDKITE_ENV_FILE=/workspace/job-env-abc123",
				"BUILDKITE_ENV_JSON_FILE=/workspace/job-env-json-abc123",
				"BUILDKITE_AGENT_JOB_TIMEOUT_FILE=/workspace/job-timeout-abc123",
			},
			contextDir: "/buildkite-shared",
			want: map[string]string{
				"BUILDKITE_ENV_FILE":               filepath.Join("/buildkite-shared", "job-env-abc123"),
				"BUILDKITE_ENV_JSON_FILE":          filepath.Join("/buildkite-shared", "job-env-json-abc123"),
				"BUILDKITE_AGENT_JOB_TIMEOUT_FILE": filepath.Join("/buildkite-shared", "job-timeout-abc123"),
			},
		},
		{
			name: "same_mount_path_is_unchanged",
			environ: []string{
				"BUILDKITE_ENV_FILE=" + filepath.Join("/workspace", "job-env-abc123"),
			},
			contextDir: "/workspace",
			want: map[string]string{
				"BUILDKITE_ENV_FILE": filepath.Join("/workspace", "job-env-abc123"),
			},
		},
		{
			name: "unrelated_vars_are_untouched",
			environ: []string{
				"BUILDKITE_COMMAND=echo hello",
			},
			contextDir: "/buildkite-shared",
			want: map[string]string{
				"BUILDKITE_COMMAND": "echo hello",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			environ := env.FromSlice(tc.environ)
			rebaseJobContextPaths(environ, tc.contextDir)

			for name, want := range tc.want {
				if got, _ := environ.Get(name); got != want {
					t.Errorf("environ.Get(%q) = %q, want %q", name, got, want)
				}
			}
		})
	}
}

func TestRebaseJobContextPathsLeavesAbsentVarsAbsent(t *testing.T) {
	t.Parallel()

	environ := env.FromSlice([]string{"BUILDKITE_COMMAND=echo hello"})
	rebaseJobContextPaths(environ, "/buildkite-shared")

	for _, name := range jobContextFileVars {
		if _, has := environ.Get(name); has {
			t.Errorf("environ.Get(%q) exists, want absent", name)
		}
	}
}
