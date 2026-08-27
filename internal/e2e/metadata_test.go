//go:build e2e

package e2e

import (
	"testing"
)

// Test that an agent can set metadata in one step and read it back
// (including via meta-data exists and meta-data get --default) in a
// later step.
func TestMetaData(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	tc := newTestCase(t, "meta_data.yaml")

	tc.startAgent()
	build := tc.triggerBuild()
	state := tc.waitForBuild(ctx, build)
	if got, want := state, "passed"; got != want {
		t.Errorf("Build state = %q, want %q", got, want)
	}
}
