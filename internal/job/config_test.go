package job

import (
	"testing"

	"github.com/buildkite/agent/v3/env"
	"github.com/google/go-cmp/cmp"
)

func TestEnvVarsAreMappedToConfig(t *testing.T) {
	t.Parallel()

	config := &ExecutorConfig{
		Repository:                   "https://original.host/repo.git",
		AutomaticArtifactUploadPaths: "llamas/",
		GitCloneFlags:                "--prune",
		GitCleanFlags:                "-v",
		AgentName:                    "myAgent",
		CleanCheckout:                false,
		PluginsAlwaysCloneFresh:      false,
		GitSubmodules:                false,
	}

	environ := env.FromSlice([]string{
		"BUILDKITE_ARTIFACT_PATHS=newpath",
		"BUILDKITE_GIT_CLONE_FLAGS=-f",
		"BUILDKITE_SOMETHING_ELSE=1",
		"BUILDKITE_REPO=https://my.mirror/repo.git",
		"BUILDKITE_CLEAN_CHECKOUT=true",
		"BUILDKITE_PLUGINS_ALWAYS_CLONE_FRESH=true",
		"BUILDKITE_GIT_SUBMODULES=true",
	})

	changes := config.ReadFromEnvironment(environ)
	wantChanges := map[string]string{
		"BUILDKITE_ARTIFACT_PATHS":             "newpath",
		"BUILDKITE_GIT_CLONE_FLAGS":            "-f",
		"BUILDKITE_REPO":                       "https://my.mirror/repo.git",
		"BUILDKITE_CLEAN_CHECKOUT":             "true",
		"BUILDKITE_PLUGINS_ALWAYS_CLONE_FRESH": "true",
		"BUILDKITE_GIT_SUBMODULES":             "true",
	}

	if diff := cmp.Diff(changes, wantChanges); diff != "" {
		t.Errorf("config.ReadFromEnvironment(environ) diff (-got +want):\n%s", diff)
	}

	if got, want := config.GitCleanFlags, "-v"; got != want {
		t.Errorf("config.GitCleanFlags = %q, want %q", got, want)
	}

	if got, want := config.Repository, "https://my.mirror/repo.git"; got != want {
		t.Errorf("config.Repository = %q, want %q", got, want)
	}

	if got, want := config.CleanCheckout, true; got != want {
		t.Errorf("config.CleanCheckout = %t, want %t", got, want)
	}

	if got, want := config.PluginsAlwaysCloneFresh, true; got != want {
		t.Errorf("config.PluginsAlwaysCloneFresh = %t, want %t", got, want)
	}

	if got, want := config.GitSubmodules, true; got != want {
		t.Errorf("config.GitSubmodules = %t, want %t", got, want)
	}
}

func TestReadFromEnvironmentIgnoresMalformedBooleans(t *testing.T) {
	t.Parallel()
	config := &ExecutorConfig{
		CleanCheckout:           true,
		PluginsAlwaysCloneFresh: false,
		GitSubmodules:           true,
	}
	environ := env.FromSlice([]string{
		"BUILDKITE_CLEAN_CHECKOUT=blarg",
		"BUILDKITE_PLUGINS_ALWAYS_CLONE_FRESH=grarg",
		"BUILDKITE_GIT_SUBMODULES=notabool",
	})
	changes := config.ReadFromEnvironment(environ)
	if len(changes) != 0 {
		t.Errorf("changes = %v, want none", changes)
	}
	if got, want := config.CleanCheckout, true; got != want {
		t.Errorf("config.CleanCheckout = %t, want %t", got, want)
	}
	if got, want := config.PluginsAlwaysCloneFresh, false; got != want {
		t.Errorf("config.PluginsAlwaysCloneFresh = %t, want %t", got, want)
	}
	if got, want := config.GitSubmodules, true; got != want {
		t.Errorf("config.GitSubmodules = %t, want %t", got, want)
	}
}

func TestGitSubmodulesBidirectionalControl(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		initial     bool
		envValue    string
		wantValue   bool
		wantChanged bool
	}{
		{
			name:        "enable submodules",
			initial:     false,
			envValue:    "true",
			wantValue:   true,
			wantChanged: true,
		},
		{
			name:        "disable submodules",
			initial:     true,
			envValue:    "false",
			wantValue:   false,
			wantChanged: true,
		},
		{
			name:        "already enabled",
			initial:     true,
			envValue:    "true",
			wantValue:   true,
			wantChanged: false,
		},
		{
			name:        "already disabled",
			initial:     false,
			envValue:    "false",
			wantValue:   false,
			wantChanged: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &ExecutorConfig{
				GitSubmodules: tt.initial,
			}

			environ := env.FromSlice([]string{
				"BUILDKITE_GIT_SUBMODULES=" + tt.envValue,
			})

			changes := config.ReadFromEnvironment(environ)

			// Verify field value updated correctly
			if got, want := config.GitSubmodules, tt.wantValue; got != want {
				t.Errorf("config.GitSubmodules = %t, want %t", got, want)
			}

			// Verify changes map reflects whether value actually changed
			if tt.wantChanged {
				wantChanges := map[string]string{
					"BUILDKITE_GIT_SUBMODULES": tt.envValue,
				}
				if diff := cmp.Diff(changes, wantChanges); diff != "" {
					t.Errorf("changes diff (-got +want):\n%s", diff)
				}
			} else {
				if len(changes) != 0 {
					t.Errorf("changes = %v, want none (value unchanged)", changes)
				}
			}
		})
	}
}
