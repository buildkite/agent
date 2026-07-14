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
		"BUILDKITE_CHECKOUT_OVERRIDE_MODE",
		"BUILDKITE_CONTAINER_COUNT",
		"BUILDKITE_GIT_MIRRORS_LOCK_TIMEOUT",
		"BUILDKITE_GIT_MIRRORS_PATH",
		"BUILDKITE_GIT_MIRROR_CHECKOUT_MODE",
		"BUILDKITE_GIT_SUBMODULE_CLONE_CONFIG",
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
		"BUILDKITE_GIT_CHECKOUT_TIMEOUT",
		"BUILDKITE_GIT_CLONE_FLAGS",
		"BUILDKITE_GIT_CLONE_MIRROR_FLAGS",
		"BUILDKITE_GIT_CLEAN_FLAGS",
		"BUILDKITE_GIT_FETCH_FLAGS",
		"BUILDKITE_GIT_MIRRORS_SKIP_UPDATE",
		"BUILDKITE_GIT_SUBMODULES",
		"BUILDKITE_GIT_SKIP_FETCH_EXISTING_COMMITS",
		"BUILDKITE_GIT_SPARSE_CHECKOUT_PATHS",
		"BUILDKITE_SKIP_CHECKOUT",
	}

	// These vars are conditionally locked: scoped, write-protected depending on
	// the checkout override mode. (Disjointness from protectedEnv is covered by
	// TestProtectedAndCheckoutScopeDisjoint.)
	for _, envVar := range scoped {
		if got := IsCheckoutOverrideScoped(envVar); !got {
			t.Errorf("IsCheckoutOverrideScoped(%q) = false, want true", envVar)
		}
	}

	// Mirror infra is agent-only: always protected and never in the override
	// scope, so a job can't relocate the mirror, stretch its lock timeout, or
	// change the mirror checkout mode. Submodule clone config is likewise
	// always protected (a `git -c` injection vector with no backend knob).
	for _, envVar := range []string{
		"BUILDKITE_GIT_MIRRORS_PATH",
		"BUILDKITE_GIT_MIRRORS_LOCK_TIMEOUT",
		"BUILDKITE_GIT_MIRROR_CHECKOUT_MODE",
		"BUILDKITE_GIT_SUBMODULE_CLONE_CONFIG",
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

func TestParseCheckoutOverrideMode(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in      string
		want    CheckoutOverrideMode
		wantErr bool
	}{
		{"", CheckoutOverrideFromJob, false}, // empty selects the default
		{"from-job", CheckoutOverrideFromJob, false},
		{"strict", CheckoutOverrideStrict, false},
		{"none", CheckoutOverrideNone, false},
		{"STRICT", CheckoutOverrideFromJob, true}, // case-sensitive
		{"lockdown", CheckoutOverrideFromJob, true},
	}

	for _, tc := range cases {
		got, err := ParseCheckoutOverrideMode(tc.in)
		if (err != nil) != tc.wantErr {
			t.Errorf("ParseCheckoutOverrideMode(%q) err = %v, wantErr %t", tc.in, err, tc.wantErr)
		}
		if got != tc.want {
			t.Errorf("ParseCheckoutOverrideMode(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestCheckoutOverrideModeStringRoundTrip(t *testing.T) {
	t.Parallel()

	for _, m := range []CheckoutOverrideMode{CheckoutOverrideFromJob, CheckoutOverrideStrict, CheckoutOverrideNone} {
		got, err := ParseCheckoutOverrideMode(m.String())
		if err != nil {
			t.Errorf("ParseCheckoutOverrideMode(%q): unexpected err %v", m.String(), err)
		}
		if got != m {
			t.Errorf("round trip via %q = %v, want %v", m.String(), got, m)
		}
	}
}

func TestCheckoutLockPredicates(t *testing.T) {
	t.Parallel()

	const (
		scoped    = "BUILDKITE_GIT_CLONE_FLAGS" // in checkoutOverrideScope
		protected = "BUILDKITE_COMMAND_EVAL"    // in protectedEnv, not scoped
		unscoped  = "MY_CUSTOM_VAR"             // in neither map
	)

	// The matrix from the design: strict locks every source; from-job locks the
	// outside-job sources (backend env, secrets) but leaves within-job sources
	// (hooks, plugins, Job API) open, matching the agent's historical behaviour;
	// none locks nothing.
	cases := []struct {
		mode           CheckoutOverrideMode
		wantFromJobEnv bool
		wantWithinJob  bool
	}{
		{CheckoutOverrideStrict, true, true},
		{CheckoutOverrideFromJob, true, false},
		{CheckoutOverrideNone, false, false},
	}

	for _, tc := range cases {
		if got := IsCheckoutLocked(scoped, tc.mode); got != tc.wantFromJobEnv {
			t.Errorf("IsCheckoutLocked(%q, %v) = %t, want %t", scoped, tc.mode, got, tc.wantFromJobEnv)
		}
		if got := IsCheckoutLockedFromWithinJob(scoped, tc.mode); got != tc.wantWithinJob {
			t.Errorf("IsCheckoutLockedFromWithinJob(%q, %v) = %t, want %t", scoped, tc.mode, got, tc.wantWithinJob)
		}

		// Non-checkout-scoped vars are never governed by the checkout predicates,
		// regardless of mode; protectedEnv handles them via IsProtected*.
		for _, name := range []string{protected, unscoped} {
			if IsCheckoutLocked(name, tc.mode) {
				t.Errorf("IsCheckoutLocked(%q, %v) = true, want false", name, tc.mode)
			}
			if IsCheckoutLockedFromWithinJob(name, tc.mode) {
				t.Errorf("IsCheckoutLockedFromWithinJob(%q, %v) = true, want false", name, tc.mode)
			}
		}
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
