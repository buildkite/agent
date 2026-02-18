//go:build e2e

package e2e

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestBasicE2E(t *testing.T) {
	ctx := t.Context()
	tc := newTestCase(t, "basic_e2e.yaml")

	tc.startAgent()
	build := tc.triggerBuild()

	// It should take much less time than 1 minute to successfully run the job.
	waitCtx, canc := context.WithTimeout(ctx, 1*time.Minute)
	defer canc()

	state := tc.waitForBuild(waitCtx, build)
	if got, want := state, "passed"; got != want {
		t.Errorf("Build state = %q, want %q", got, want)
	}

	logs := tc.fetchLogs(ctx, build)
	if !strings.Contains(logs, "hello world") {
		t.Errorf("tc.fetchLogs(ctx, build %q) logs as follows, did not contain 'hello world'\n%s", build.ID, logs)
	}
}

func TestBasicE2E_PingOnly(t *testing.T) {
	ctx := t.Context()
	tc := newTestCase(t, "basic_e2e.yaml")

	tc.startAgent("--ping-mode=ping-only")
	build := tc.triggerBuild()

	// It should take much less time than 1 minute to successfully run the job.
	waitCtx, canc := context.WithTimeout(ctx, 1*time.Minute)
	defer canc()

	state := tc.waitForBuild(waitCtx, build)
	if got, want := state, "passed"; got != want {
		t.Errorf("Build state = %q, want %q", got, want)
	}

	logs := tc.fetchLogs(ctx, build)
	if !strings.Contains(logs, "hello world") {
		t.Errorf("tc.fetchLogs(ctx, build %q) logs as follows, did not contain 'hello world'\n%s", build.ID, logs)
	}
}

func TestBasicE2E_StreamOnly(t *testing.T) {
	ctx := t.Context()
	tc := newTestCase(t, "basic_e2e.yaml")

	tc.startAgent(
		"--ping-mode=stream-only",
		"--endpoint=https://agent-edge.buildkite.com/v3",
	)
	build := tc.triggerBuild()

	// It should take much less time than 1 minute to successfully run the job.
	waitCtx, canc := context.WithTimeout(ctx, 1*time.Minute)
	defer canc()

	state := tc.waitForBuild(waitCtx, build)
	if got, want := state, "passed"; got != want {
		t.Errorf("Build state = %q, want %q", got, want)
	}

	logs := tc.fetchLogs(ctx, build)
	if !strings.Contains(logs, "hello world") {
		t.Errorf("tc.fetchLogs(ctx, build %q) logs as follows, did not contain 'hello world'\n%s", build.ID, logs)
	}
}
