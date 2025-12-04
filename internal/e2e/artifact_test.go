//go:build e2e

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
