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
		"BUILDKITE_GIT_MIRRORS_LOCK_TIMEOUT",
		"BUILDKITE_GIT_MIRRORS_PATH",
		"BUILDKITE_HOOKS_PATH",
		"BUILDKITE_KUBERNETES_EXEC",
		"BUILDKITE_LOCAL_HOOKS_ENABLED",
		"BUILDKITE_NO_CHECKOUT_OVERRIDE",
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
		"BUILDKITE_GIT_CLONE_FLAGS",
		"BUILDKITE_GIT_SUBMODULES",
		"BUILDKITE_SKIP_CHECKOUT",
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

func TestCheckoutOverrideScope(t *testing.T) {
	t.Parallel()

	scoped := []string{
		"BUILDKITE_GIT_CHECKOUT_FLAGS",
		"BUILDKITE_GIT_CLONE_FLAGS",
		"BUILDKITE_GIT_CLONE_MIRROR_FLAGS",
		"BUILDKITE_GIT_CLEAN_FLAGS",
		"BUILDKITE_GIT_FETCH_FLAGS",
		"BUILDKITE_GIT_SUBMODULE_CLONE_CONFIG",
		"BUILDKITE_GIT_MIRRORS_SKIP_UPDATE",
		"BUILDKITE_GIT_MIRROR_CHECKOUT_MODE",
		"BUILDKITE_GIT_SUBMODULES",
		"BUILDKITE_GIT_SKIP_FETCH_EXISTING_COMMITS",
		"BUILDKITE_SKIP_CHECKOUT",
	}

	// These vars are conditionally locked: scoped, write-protected only when
	// no-checkout-override is enabled. (Disjointness from protectedEnv is
	// covered by TestProtectedAndCheckoutScopeDisjoint.)
	for _, envVar := range scoped {
		if got := IsCheckoutOverrideScoped(envVar); !got {
			t.Errorf("IsCheckoutOverrideScoped(%q) = false, want true", envVar)
		}
	}

	// Mirror infra is agent-only: always protected and never in the override
	// scope, so a job can't relocate the mirror or stretch its lock timeout.
	for _, envVar := range []string{
		"BUILDKITE_GIT_MIRRORS_PATH",
		"BUILDKITE_GIT_MIRRORS_LOCK_TIMEOUT",
		"BUILDKITE_SSH_KEYSCAN",
		"BUILDKITE_COMMAND_EVAL",
	} {
		if got := IsProtected(envVar); !got {
			t.Errorf("IsProtected(%q) = false, want true", envVar)
		}
		if got := IsCheckoutOverrideScoped(envVar); got {
			t.Errorf("IsCheckoutOverrideScoped(%q) = true, want false", envVar)
		}
	}

	// An unrecognised var is neither protected nor checkout-scoped.
	if IsProtected("MY_CUSTOM_VAR") {
		t.Errorf("IsProtected(MY_CUSTOM_VAR) = true, want false")
	}
	if IsCheckoutOverrideScoped("MY_CUSTOM_VAR") {
		t.Errorf("IsCheckoutOverrideScoped(MY_CUSTOM_VAR) = true, want false")
	}
}

func TestProtectedAndCheckoutScopeDisjoint(t *testing.T) {
	t.Parallel()

	// A var must sit in exactly one tier. The enforcement sites read the two
	// maps through different predicates, so a var in both would be locked
	// inconsistently across hooks, secrets, the Job API, and config refresh.
	for name := range checkoutOverrideScope {
		if _, ok := protectedEnv[name]; ok {
			t.Errorf("%q is in both protectedEnv and checkoutOverrideScope; it must sit in exactly one tier", name)
		}
	}
}
