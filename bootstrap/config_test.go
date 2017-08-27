package bootstrap

import (
	"reflect"
	"sort"
	"testing"

	"github.com/buildkite/agent/env"
)

func TestEnvVarsAreMappedToConfig(t *testing.T) {
	config := &Config{
		AutomaticArtifactUploadPaths: "llamas/",
		GitCloneFlags:                "--prune",
		GitCleanFlags:                "-v",
		AgentName:                    "myAgent",
	}

	environ := env.FromSlice([]string{
		"BUILDKITE_ARTIFACT_PATHS=newpath",
		"BUILDKITE_GIT_CLONE_FLAGS=-f",
		"BUILDKITE_SOMETHING_ELSE=1",
	})

	changes := config.ReadFromEnvironment(environ)
	expected := []string{
		"BUILDKITE_ARTIFACT_PATHS",
		"BUILDKITE_GIT_CLONE_FLAGS",
	}

	sort.Sort(sort.StringSlice(expected))
	sort.Sort(sort.StringSlice(changes))

	if !reflect.DeepEqual(expected, changes) {
		t.Fatalf("%#v wasn't equal to %#v", expected, changes)
	}

	if expected := "newpath"; config.AutomaticArtifactUploadPaths != expected {
		t.Fatalf("Expected AutomaticArtifactUploadPaths to be %v, got %v",
			expected, config.AutomaticArtifactUploadPaths)
	}

	if expected := "-f"; config.GitCloneFlags != expected {
		t.Fatalf("Expected GitCloneFlags to be %v, got %v",
			expected, config.GitCloneFlags)
	}

	if expected := "-v"; config.GitCleanFlags != expected {
		t.Fatalf("Expected GitCleanFlags to be %v, got %v",
			expected, config.GitCleanFlags)
	}
}
