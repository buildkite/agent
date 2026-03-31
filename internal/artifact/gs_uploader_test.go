package artifact

import (
	"os"
	"strings"
	"testing"

	"github.com/buildkite/agent/v4/api"
)

func TestGSUploaderURLAppendsPathSuffix(t *testing.T) {
	t.Setenv("BUILDKITE_GCS_PATH_SUFFIX", ";tab=live_object")
	for _, key := range []string{"BUILDKITE_GCS_ACCESS_HOST", "BUILDKITE_GCS_PATH_PREFIX"} {
		if orig, ok := os.LookupEnv(key); ok {
			os.Unsetenv(key) //nolint:errcheck // Best-effort.
			t.Cleanup(func() {
				os.Setenv(key, orig) //nolint:errcheck // Best-effort restore.
			})
		}
	}

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

func TestGSUploaderArtifactPath(t *testing.T) {
	tests := []struct {
		name         string
		bucketPath   string
		artifactPath string
		want         string
	}{
		{
			name:         "empty bucket path, top-level artifact",
			bucketPath:   "",
			artifactPath: "index.html",
			want:         "index.html",
		},
		{
			name:         "empty bucket path, nested artifact",
			bucketPath:   "",
			artifactPath: "nested/index.html",
			want:         "nested/index.html",
		},
		{
			name:         "non-empty bucket path, top-level artifact",
			bucketPath:   "prefix",
			artifactPath: "index.html",
			want:         "prefix/index.html",
		},
		{
			name:         "non-empty bucket path, nested artifact",
			bucketPath:   "artifacts/build",
			artifactPath: "nested/index.html",
			want:         "artifacts/build/nested/index.html",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			u := &GSUploader{BucketName: "my-bucket", BucketPath: tc.bucketPath}
			got := u.artifactPath(&api.Artifact{Path: tc.artifactPath})
			if got != tc.want {
				t.Errorf("artifactPath() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestGSUploaderArtifactPathNoLeadingSlash is a regression guard for the
// bucket-root upload bug: an empty BucketPath must not produce a leading slash
// or a double slash in the object key.
func TestGSUploaderArtifactPathNoLeadingSlash(t *testing.T) {
	u := &GSUploader{BucketName: "my-bucket", BucketPath: ""}
	got := u.artifactPath(&api.Artifact{Path: "index.html"})
	if strings.HasPrefix(got, "/") {
		t.Errorf("artifactPath() = %q, must not start with a slash", got)
	}
	if strings.Contains(got, "//") {
		t.Errorf("artifactPath() = %q, must not contain a double slash", got)
	}
}

// TestGSUploaderUploadKeyMatchesURL asserts that, for a bucket-root
// destination, the object name used at upload (artifactPath) equals the key
// portion of the generated download URL (after the host and bucket name), so
// the two can never diverge again. Both the trailing-slash and no-slash forms
// of a bucket-root destination are covered.
func TestGSUploaderUploadKeyMatchesURL(t *testing.T) {
	// URL() reads BUILDKITE_GCS_ACCESS_HOST, BUILDKITE_GCS_PATH_PREFIX, and
	// BUILDKITE_GCS_PATH_SUFFIX via os.LookupEnv; the expected prefix and key
	// below assume the default environment, so unset them for the duration of the test.
	for _, key := range []string{"BUILDKITE_GCS_ACCESS_HOST", "BUILDKITE_GCS_PATH_PREFIX", "BUILDKITE_GCS_PATH_SUFFIX"} {
		if orig, ok := os.LookupEnv(key); ok {
			os.Unsetenv(key) //nolint:errcheck // Best-effort.
			t.Cleanup(func() {
				os.Setenv(key, orig) //nolint:errcheck // Best-effort restore.
			})
		}
	}

	for _, dest := range []string{"gs://my-bucket", "gs://my-bucket/"} {
		t.Run(dest, func(t *testing.T) {
			bucket, bucketPath := ParseGSDestination(dest)
			u := &GSUploader{BucketName: bucket, BucketPath: bucketPath}
			artifact := &api.Artifact{Path: "index.html"}

			uploadKey := u.artifactPath(artifact)

			url := u.URL(artifact)
			prefix := "https://storage.googleapis.com/" + bucket + "/"
			urlKey, ok := strings.CutPrefix(url, prefix)
			if !ok {
				t.Fatalf("URL() = %q, want prefix %q", url, prefix)
			}

			if uploadKey != urlKey {
				t.Errorf("upload key %q does not match URL key %q (URL %q)", uploadKey, urlKey, url)
			}
		})
	}
}
