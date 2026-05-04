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

// Test that downloading a customer-S3 artifact produces byte-identical
// contents on both the default multipart path and the --no-s3-multipart-download
// kill-switch path (verified by sha256sum -c in the fixture). The fixture
// artifact is sized to force multiple ranged GETs at the configured 8 MiB
// part size so the multipart fan-out is actually exercised; the no-multipart
// step proves the BUILDKITE_NO_S3_MULTIPART_DOWNLOAD env var falls back to
// the legacy single-stream path.
//
// Test name structure is load-bearing: the IAM trust policy on the role
// assumed by the fixture's aws-assume-role-with-web-identity plugin pins
// pipeline_slug to "testartifactuploaddownload-custombucket-*", so the test
// name must produce a slug starting with that prefix (Buildkite converts
// underscores to hyphens during slug derivation).
func TestArtifactUploadDownload_CustomBucket_Multipart(t *testing.T) {
	ctx := t.Context()
	tc := newTestCase(t, "artifact_multipart_s3_download.yaml")

	tc.startAgent()
	build := tc.triggerBuild()
	state := tc.waitForBuild(ctx, build)

	if got, want := state, "passed"; got != want {
		t.Errorf("Build state = %q, want %q", got, want)
	}
}
