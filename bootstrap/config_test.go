package bootstrap

import (
	"reflect"
	"testing"

	"github.com/buildkite/agent/v3/env"
)

func TestEnvVarsAreMappedToConfig(t *testing.T) {
	t.Parallel()

	config := &Config{
		Repository:                   "https://original.host/repo.git",
		AutomaticArtifactUploadPaths: "llamas/",
		GitCloneFlags:                "--prune",
		GitCleanFlags:                "-v",
		AgentName:                    "myAgent",
		CleanCheckout:                false,
	}

	environ := env.FromSlice([]string{
		"BUILDKITE_ARTIFACT_PATHS=newpath",
		"BUILDKITE_GIT_CLONE_FLAGS=-f",
		"BUILDKITE_SOMETHING_ELSE=1",
		"BUILDKITE_REPO=https://my.mirror/repo.git",
		"BUILDKITE_CLEAN_CHECKOUT=true",
	})

	changes := config.ReadFromEnvironment(environ)
	expected := map[string]string{
		"BUILDKITE_ARTIFACT_PATHS":  "newpath",
		"BUILDKITE_GIT_CLONE_FLAGS": "-f",
		"BUILDKITE_REPO":            "https://my.mirror/repo.git",
		"BUILDKITE_CLEAN_CHECKOUT":  "true",
	}

	if !reflect.DeepEqual(expected, changes) {
		t.Fatalf("%#v wasn't equal to %#v", expected, changes)
	}

	if expected := "-v"; config.GitCleanFlags != expected {
		t.Fatalf("Expected GitCleanFlags to be %v, got %v",
			expected, config.GitCleanFlags)
	}

	if expected := "https://my.mirror/repo.git"; config.Repository != expected {
		t.Fatalf("Expected Repository to be %v, got %v",
			expected, config.Repository)
	}

	if expected := true; config.CleanCheckout != expected {
		t.Fatalf("Expected Repository to be %v, got %v",
			expected, config.Repository)
	}
}

func TestReadFromEnvironmentIgnoresMalformedBooleans(t *testing.T) {
	t.Parallel()
	config := &Config{
		CleanCheckout: true,
	}
	environ := env.FromSlice([]string{
		"BUILDKITE_CLEAN_CHECKOUT=blarg",
	})
	changes := config.ReadFromEnvironment(environ)
	if len(changes) != 0 {
		t.Fatalf("expected no changes, got %#v", changes)
	}
	if expected := true; config.CleanCheckout != expected {
		t.Fatalf("Expected %v, got %v", expected, config.CleanCheckout)
	}
}
