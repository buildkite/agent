package env

import (
	"runtime"
	"testing"
)

func TestProtectedEnv(t *testing.T) {
	// Test that ProtectedEnv contains the expected variables
	expectedProtected := []string{
		"BUILDKITE_AGENT_ACCESS_TOKEN",
		"BUILDKITE_AGENT_DEBUG",
		"BUILDKITE_AGENT_ENDPOINT",
		"BUILDKITE_AGENT_PID",
		"BUILDKITE_BIN_PATH",
		"BUILDKITE_BUILD_PATH",
		"BUILDKITE_COMMAND_EVAL",
		"BUILDKITE_CONFIG_PATH",
		"BUILDKITE_CONTAINER_COUNT",
		"BUILDKITE_GIT_CLEAN_FLAGS",
		"BUILDKITE_GIT_CLONE_FLAGS",
		"BUILDKITE_GIT_CLONE_MIRROR_FLAGS",
		"BUILDKITE_GIT_FETCH_FLAGS",
		"BUILDKITE_GIT_MIRRORS_LOCK_TIMEOUT",
		"BUILDKITE_GIT_MIRRORS_PATH",
		"BUILDKITE_GIT_MIRRORS_SKIP_UPDATE",
		"BUILDKITE_GIT_SUBMODULES",
		"BUILDKITE_HOOKS_PATH",
		"BUILDKITE_KUBERNETES_EXEC",
		"BUILDKITE_LOCAL_HOOKS_ENABLED",
		"BUILDKITE_PLUGINS_ENABLED",
		"BUILDKITE_PLUGINS_PATH",
		"BUILDKITE_SHELL",
		"BUILDKITE_HOOKS_SHELL",
		"BUILDKITE_SSH_KEYSCAN",
	}

	// Verify that all expected variables are protected
	for _, envVar := range expectedProtected {
		if got, want := IsProtected(envVar), true; got != want {
			t.Errorf("IsProtected(%q, false) = %t, want %t", envVar, got, want)
		}
	}

	// Verify that non-protected variables are not protected
	nonProtected := []string{
		"MY_CUSTOM_VAR",
		"SECRET_KEY",
		"DATABASE_URL",
		"API_TOKEN",
		"BUILDKITE_BRANCH",  // This is a standard build env var, not protected
		"BUILDKITE_COMMIT",  // This is a standard build env var, not protected
		"BUILDKITE_MESSAGE", // This is a standard build env var, not protected
	}

	for _, envVar := range nonProtected {
		if got, want := IsProtected(envVar), false; got != want {
			t.Errorf("IsProtected(%q, false) = %t, want %t", envVar, got, want)
		}
	}
}

func TestIsProtected_Normalised(t *testing.T) {
	name := "buildkite_command_eval"
	got := IsProtected(name)
	want := runtime.GOOS == "windows"

	if got != want {
		t.Errorf("IsProtected(%q) = %t, want %t", name, got, want)
	}
}
