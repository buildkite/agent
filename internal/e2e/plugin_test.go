//go:build e2e

package e2e

import (
	"fmt"
	"strings"
	"testing"
)

func TestPluginE2E(t *testing.T) {
	ctx := t.Context()
	tc := newTestCase(t, "repeated_plugin.yaml")

	tc.startAgent()
	build := tc.triggerBuild()
	state := tc.waitForBuild(ctx, build)
	if got, want := state, "passed"; got != want {
		t.Errorf("Build state = %q, want %q", got, want)
	}

	logs := tc.fetchLogs(ctx, build)

	hooks := []string{"environment", "pre-checkout", "post-checkout", "pre-command", "post-command"}
	for _, h := range hooks {
		needle := fmt.Sprintf("Hello from the plugin-test-plugin %s hook", h)
		if got, want := strings.Count(logs, needle), 2; got != want {
			t.Errorf("tc.fetchLogs(ctx, build %q) logs as follows, contained %d copies of %q, want %d", build.ID, got, needle, want)
		}
	}
	t.Log(logs)
}
