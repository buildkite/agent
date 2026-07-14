package job

import (
	"slices"
	"strings"
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
		GitSparseCheckoutPaths:       []string{"old-path/"},
		GitCleanFlags:                "-v",
		GitSSHKey:                    "original-key",
		AgentName:                    "myAgent",
		CleanCheckout:                false,
		PluginsAlwaysCloneFresh:      false,
		GitSubmodules:                false,
	}

	environ := env.FromSlice([]string{
		"BUILDKITE_ARTIFACT_PATHS=newpath",
		"BUILDKITE_GIT_CLONE_FLAGS=-f",
		"BUILDKITE_GIT_SPARSE_CHECKOUT_PATHS=.buildkite/,src/",
		"BUILDKITE_SOMETHING_ELSE=1",
		"BUILDKITE_REPO=https://my.mirror/repo.git",
		"BUILDKITE_CLEAN_CHECKOUT=true",
		"BUILDKITE_GIT_SSH_KEY=new-key",
		"BUILDKITE_PLUGINS_ALWAYS_CLONE_FRESH=true",
		"BUILDKITE_GIT_SUBMODULES=true",
	})

	changes := config.ReadFromEnvironment(environ)
	wantChanges := map[string]string{
		"BUILDKITE_ARTIFACT_PATHS":             "newpath",
		"BUILDKITE_GIT_CLONE_FLAGS":            "-f",
		"BUILDKITE_GIT_SPARSE_CHECKOUT_PATHS":  ".buildkite/,src/",
		"BUILDKITE_REPO":                       "https://my.mirror/repo.git",
		"BUILDKITE_CLEAN_CHECKOUT":             "true",
		"BUILDKITE_GIT_SSH_KEY":                "new-key",
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

	if got, want := config.GitSSHKey, "new-key"; got != want {
		t.Errorf("config.GitSSHKey = %q, want %q", got, want)
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

	if got := len(config.GitSparseCheckoutPaths); got != 2 {
		t.Fatalf("len(config.GitSparseCheckoutPaths) = %d, want 2 (%q)", got, strings.Join(config.GitSparseCheckoutPaths, ","))
	}
	if got, want := strings.Join(config.GitSparseCheckoutPaths, ","), ".buildkite/,src/"; got != want {
		t.Errorf("config.GitSparseCheckoutPaths = %q, want %q", got, want)
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

func TestReadFromEnvironmentDoesNotRefreshCheckoutOverrideMode(t *testing.T) {
	t.Parallel()

	config := &ExecutorConfig{CheckoutOverrideMode: env.CheckoutOverrideStrict}
	environ := env.FromSlice([]string{"BUILDKITE_CHECKOUT_OVERRIDE_MODE=none"})

	changes := config.ReadFromEnvironment(environ)
	if len(changes) != 0 {
		t.Errorf("changes = %v, want none", changes)
	}
	if got, want := config.CheckoutOverrideMode, env.CheckoutOverrideStrict; got != want {
		t.Errorf("config.CheckoutOverrideMode = %v, want %v", got, want)
	}
}

func TestReadFromEnvironmentSkipsCheckoutScopedVarsWhenCheckoutLocked(t *testing.T) {
	t.Parallel()

	config := &ExecutorConfig{
		CheckoutOverrideMode: env.CheckoutOverrideStrict,
		SkipCheckout:         false,
		GitCloneFlags:        "-v",
	}
	environ := env.FromSlice([]string{
		"BUILDKITE_SKIP_CHECKOUT=true",
		"BUILDKITE_GIT_CLONE_FLAGS=--mirror",
	})

	changes := config.ReadFromEnvironment(environ)
	if len(changes) != 0 {
		t.Errorf("changes = %v, want none", changes)
	}
	if got, want := config.SkipCheckout, false; got != want {
		t.Errorf("config.SkipCheckout = %t, want %t", got, want)
	}
	if got, want := config.GitCloneFlags, "-v"; got != want {
		t.Errorf("config.GitCloneFlags = %q, want %q", got, want)
	}
}

func TestReadFromEnvironmentRefreshesCheckoutScopedVarsUnderFromJob(t *testing.T) {
	t.Parallel()

	// from-job lets hooks/plugins reconfigure checkout, so ReadFromEnvironment
	// must apply their checkout-scoped changes rather than skip them.
	config := &ExecutorConfig{
		CheckoutOverrideMode: env.CheckoutOverrideFromJob,
		GitCloneFlags:        "-v",
	}
	environ := env.FromSlice([]string{"BUILDKITE_GIT_CLONE_FLAGS=--mirror"})

	changes := config.ReadFromEnvironment(environ)
	if got, want := config.GitCloneFlags, "--mirror"; got != want {
		t.Errorf("config.GitCloneFlags = %q, want %q", got, want)
	}
	if _, ok := changes["BUILDKITE_GIT_CLONE_FLAGS"]; !ok {
		t.Errorf("changes = %v, want it to contain BUILDKITE_GIT_CLONE_FLAGS", changes)
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

func TestReadFromEnvironmentSlice(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		initial     []string
		envValue    string
		wantChanged bool
		wantField   []string
	}{
		{"nil unchanged when env empty", nil, "", false, nil},
		{"slice unchanged matches CSV", []string{"protocol.file.allow=always", "http.sslVerify=false"}, "protocol.file.allow=always,http.sslVerify=false", false, []string{"protocol.file.allow=always", "http.sslVerify=false"}},
		{"nil to non-empty CSV", nil, "a,b", true, []string{"a", "b"}},
		{"non-empty cleared by empty env", []string{"a"}, "", true, nil},
		{"different values", []string{"a"}, "b", true, []string{"b"}},
		{"reorder counts as change", []string{"a", "b"}, "b,a", true, []string{"b", "a"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			config := &ExecutorConfig{GitSubmoduleCloneConfig: tc.initial}
			environ := env.FromSlice([]string{
				"BUILDKITE_GIT_SUBMODULE_CLONE_CONFIG=" + tc.envValue,
			})
			changes := config.ReadFromEnvironment(environ)

			_, gotChanged := changes["BUILDKITE_GIT_SUBMODULE_CLONE_CONFIG"]
			if gotChanged != tc.wantChanged {
				t.Errorf("changed = %v, want %v (changes=%v)", gotChanged, tc.wantChanged, changes)
			}
			if !slices.Equal(config.GitSubmoduleCloneConfig, tc.wantField) {
				t.Errorf("GitSubmoduleCloneConfig = %v, want %v", config.GitSubmoduleCloneConfig, tc.wantField)
			}
		})
	}
}
