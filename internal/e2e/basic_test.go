//go:build e2e

package e2e

import "testing"

func TestBasicE2E(t *testing.T) {
	ctx := t.Context()
	tc := newTestCase(t, `
agents:
  queue: {{.queue}}
steps:
  - command: echo hello world
`)

	tc.startAgent()

	build := tc.triggerBuild()
	state, err := tc.waitForBuild(ctx, build)
	if err != nil {
		t.Fatalf("tc.waitForBuild(build %s) error = %v", build.ID, err)
	}
	if got, want := state, "passed"; got != want {
		t.Errorf("Build state = %q, want %q", got, want)
	}

	// TODO: add ability to inspect job logs
}
