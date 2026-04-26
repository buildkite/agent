//go:build e2e

// Note to external contributors: Many test cases in this file require
// access to specific Buildkite organization resources and may not work
// in your local environment. These tests can be safely skipped during
// local development.

package e2e

import (
	"testing"
)

// Test that an agent can upload and download an artifact across different steps in the same build
func TestArtifactUploadDownload(t *testing.T) {
	ctx := t.Context()

	tc := newTestCase(t, "artifact_upload_download.yaml")

	tc.startAgent()
	build := tc.triggerBuild()
	state := tc.waitForBuild(ctx, build)
	if got, want := state, "passed"; got != want {
		t.Errorf("Build state = %q, want %q", got, want)
	}
}

// Test that an agent can upload and download artifact to/from a customer-managed S3 bucket
func TestArtifactUploadDownload_CustomBucket(t *testing.T) {
	ctx := t.Context()
	tc := newTestCase(t, "artifact_custom_s3_bucket.yaml")

	tc.startAgent()
	build := tc.triggerBuild()
	state := tc.waitForBuild(ctx, build)

	if got, want := state, "passed"; got != want {
		t.Errorf("Build state = %q, want %q", got, want)
	}
}

// Test that we can upload/download artifact using a custom GCS bucket.
// Everything that gets uploaded here gets auto removed in 30 days.
func TestArtifactUploadDownload_GCS(t *testing.T) {
	ctx := t.Context()
	tc := newTestCase(t, "artifact_custom_gcs_bucket.yaml")

	tc.startAgent()
	build := tc.triggerBuild()
	state := tc.waitForBuild(ctx, build)

	if got, want := state, "passed"; got != want {
		t.Errorf("Build state = %q, want %q", got, want)
	}
}

// Test that an agent can upload and download many artifacts (100 files).
// This exercises the batch creator iterator producing multiple batches (batch size = 30).
func TestArtifactUploadMany(t *testing.T) {
	ctx := t.Context()

	tc := newTestCase(t, "artifact_upload_many.yaml")

	tc.startAgent()
	build := tc.triggerBuild()
	state := tc.waitForBuild(ctx, build)
	if got, want := state, "passed"; got != want {
		t.Errorf("Build state = %q, want %q", got, want)
	}
}

// Test that artifact upload with --glob-resolve-follow-symlinks follows symlinked directories.
// Regression test for https://github.com/buildkite/agent/issues/3826
func TestArtifactUploadFollowSymlinks(t *testing.T) {
	ctx := t.Context()

	tc := newTestCase(t, "artifact_upload_symlink_glob.yaml")

	tc.startAgent()
	build := tc.triggerBuild()
	state := tc.waitForBuild(ctx, build)
	if got, want := state, "passed"; got != want {
		t.Errorf("Build state = %q, want %q", got, want)
	}
}

// Test that we can upload/downdload artifact using a custom Azure Blob storage
// container.
// Everything that gets uploaded here gets auto removed in 30 days.
func TestArtifactUploadDownload_Azure(t *testing.T) {
	ctx := t.Context()
	tc := newTestCase(t, "artifact_custom_azure_storage.yaml")

	tc.startAgent()
	build := tc.triggerBuild()
	state := tc.waitForBuild(ctx, build)

	if got, want := state, "passed"; got != want {
		t.Errorf("Build state = %q, want %q", got, want)
	}
}
