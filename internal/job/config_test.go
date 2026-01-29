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
