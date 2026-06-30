package artifact

import (
	"os"
	"testing"

	"github.com/buildkite/agent/v3/api"
)

func TestGSUploaderURLAppendsPathSuffix(t *testing.T) {
	t.Setenv("BUILDKITE_GCS_PATH_SUFFIX", ";tab=live_object")

	u := &GSUploader{
		BucketName: "my-bucket",
		BucketPath: "artifacts/my-pipeline/build-1/job-1",
	}
	artifact := &api.Artifact{Path: "index.html"}

	got := u.URL(artifact)
	want := "https://storage.googleapis.com/my-bucket/artifacts/my-pipeline/build-1/job-1/index.html;tab=live_object"
	if got != want {
		t.Errorf("URL() = %q, want %q", got, want)
	}
}

func TestGSUploaderURLWithoutPathSuffix(t *testing.T) {
	// Defensively ensure the suffix var is not set for this test.
	if v, ok := os.LookupEnv("BUILDKITE_GCS_PATH_SUFFIX"); ok {
		os.Unsetenv("BUILDKITE_GCS_PATH_SUFFIX")
		t.Cleanup(func() { os.Setenv("BUILDKITE_GCS_PATH_SUFFIX", v) })
	}

	u := &GSUploader{
		BucketName: "my-bucket",
		BucketPath: "foo/bar",
	}
	artifact := &api.Artifact{Path: "index.html"}

	got := u.URL(artifact)
	want := "https://storage.googleapis.com/my-bucket/foo/bar/index.html"
	if got != want {
		t.Errorf("URL() = %q, want %q", got, want)
	}
}

func TestGSUploaderPathSuffixDoesNotAffectObjectName(t *testing.T) {
	t.Setenv("BUILDKITE_GCS_PATH_SUFFIX", ";tab=live_object")

	u := &GSUploader{
		BucketName: "my-bucket",
		BucketPath: "foo/bar",
	}
	artifact := &api.Artifact{Path: "index.html"}

	// artifactPath is the object name used at upload — it must NOT include the suffix.
	if got, want := u.artifactPath(artifact), "foo/bar/index.html"; got != want {
		t.Errorf("artifactPath() = %q, want %q (suffix must not leak into object name)", got, want)
	}
}

func TestParseGSDestination(t *testing.T) {
	tests := []struct {
		dest, bucket, path string
	}{
		{
			dest:   "gs://my-bucket-name/foo/bar",
			bucket: "my-bucket-name",
			path:   "foo/bar",
		},
		{
			dest:   "gs://starts-with-an-s/and-this-is-its/folder",
			bucket: "starts-with-an-s",
			path:   "and-this-is-its/folder",
		},
	}
	for _, tc := range tests {
		bucket, path := ParseGSDestination(tc.dest)
		if bucket != tc.bucket || path != tc.path {
			t.Errorf("ParseGSDestination(%q) = (%q, %q), want (%q, %q)", tc.dest, bucket, path, tc.bucket, tc.path)
		}
	}
}
