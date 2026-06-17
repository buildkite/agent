//go:build e2e

// Note to external contributors: Many test cases in this file require
// access to specific Buildkite organization resources and may not work
// in your local environment. These tests can be safely skipped during
// local development.

package e2e

import (
	"testing"
)

// Test that an agent can save a cache and restore it (with round-tripped
// content) in a dependent step, using a customer-managed S3 cache store.
//
// Test name structure is load-bearing: the IAM trust policy on the role
// assumed by the fixture's aws-assume-role-with-web-identity plugin pins
// pipeline_slug to a prefix, so the test name must produce a slug starting
// with that prefix (Buildkite converts underscores to hyphens during slug
// derivation, and the slug is lower(t.Name() + "-" + jobID)). The trust
// policy allows "testcache*" to cover this and future cache test variants.
func TestCacheBasicSaveRestore(t *testing.T) {
	ctx := t.Context()

	tc := newTestCase(t, "cache_save_restore.yaml")

	tc.startAgent()
	build := tc.triggerBuild()
	state := tc.waitForBuild(ctx, build)
	if got, want := state, "passed"; got != want {
		t.Errorf("Build state = %q, want %q", got, want)
	}
}
