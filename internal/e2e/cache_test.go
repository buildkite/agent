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

// Test that a saved cache round-trips filesystem details: nested and empty
// directories, file permissions, symlinks, and an absolute (home) target path.
// The restore step deletes the targets before restoring so the assertions
// prove the cache, not leftover state from the save step.
func TestCacheFilesystemIntegrity(t *testing.T) {
	ctx := t.Context()

	tc := newTestCase(t, "cache_filesystem_integrity.yaml")

	tc.startAgent()
	build := tc.triggerBuild()
	state := tc.waitForBuild(ctx, build)
	if got, want := state, "passed"; got != want {
		t.Errorf("Build state = %q, want %q", got, want)
	}
}

// Test that a cache_key's fallback_limit makes trailing parts optional on
// restore. Two caches share a BUILD_ID-anchored key but differ in a trailing
// CACHE_VARIANT part that changes between save and restore: the cache that
// declares fallback_limit on BUILD_ID falls back to the BUILD_ID match and
// restores, while the cache without fallback_limit treats the variant mismatch
// as a hard miss.
func TestCacheKeyFallback(t *testing.T) {
	ctx := t.Context()

	tc := newTestCase(t, "cache_fallback.yaml")

	tc.startAgent()
	build := tc.triggerBuild()
	state := tc.waitForBuild(ctx, build)
	if got, want := state, "passed"; got != want {
		t.Errorf("Build state = %q, want %q", got, want)
	}
}

// Test the cache-busting recovery path: when a blob is deleted from storage but
// the registry entry survives (split-brain), restore degrades to a clean miss
// and invalidates the stale entry, so a subsequent save re-uploads and a final
// restore hits. The fixture nukes the blob from S3 between save and restore.
func TestCacheMissingBlobReupload(t *testing.T) {
	ctx := t.Context()

	tc := newTestCase(t, "cache_missing_blob_reupload.yaml")

	tc.startAgent()
	build := tc.triggerBuild()
	state := tc.waitForBuild(ctx, build)
	if got, want := state, "passed"; got != want {
		t.Errorf("Build state = %q, want %q", got, want)
	}
}
