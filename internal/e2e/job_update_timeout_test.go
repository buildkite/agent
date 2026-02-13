//go:build e2e

package e2e

import (
	"testing"
)

// TestJobUpdateTimeout verifies that a job which reduces its timeout from 10
// minutes to 1 minute via `job update timeout`, then sleeps for 5 minutes,
// exceeds the new timeout and fails.
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
