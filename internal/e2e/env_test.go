//go:build e2e

package e2e

import (
	"testing"
)

// Test that pipeline-level and step-level env vars are both applied to a
// job, with step-level env taking precedence over pipeline-level env.
func TestEnvPrecedence(t *testing.T) {
	ctx := t.Context()

	tc := newTestCase(t, "env_precedence.yaml")

	tc.startAgent()
	build := tc.triggerBuild()
	state := tc.waitForBuild(ctx, build)
	if got, want := state, "passed"; got != want {
		t.Errorf("Build state = %q, want %q", got, want)
	}
}
