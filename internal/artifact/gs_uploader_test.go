package artifact

import (
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/api"
)

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
// the two can never diverge again.
func TestGSUploaderUploadKeyMatchesURL(t *testing.T) {
	bucket, bucketPath := ParseGSDestination("gs://my-bucket")
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
}
