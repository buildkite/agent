//go:build e2e

package e2e

import (
	"strings"
	"testing"
)

func TestBasicE2E(t *testing.T) {
	ctx := t.Context()
	tc := newTestCase(t, "basic_e2e.yaml")

	tc.startAgent()
	build := tc.triggerBuild()
	state := tc.waitForBuild(ctx, build)
	if got, want := state, "passed"; got != want {
		t.Errorf("Build state = %q, want %q", got, want)
	}

	logs := tc.fetchLogs(ctx, build)
	if !strings.Contains(logs, "hello world") {
		t.Errorf("tc.fetchLogs(ctx, build %q) logs as follows, did not contain 'hello world'\n%s", build.ID, logs)
	}
}
