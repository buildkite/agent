//go:build e2e

package e2e

import (
	"testing"
)

func TestJobUpdateTimeout(t *testing.T) {
	ctx := t.Context()
	tc := newTestCase(t, "job_update_timeout.yaml")

	tc.startAgent()
	build := tc.triggerBuild()
	state := tc.waitForBuild(ctx, build)
	if got, want := state, "failed"; got != want {
		t.Errorf("Build state = %q, want %q", got, want)
	}
}
