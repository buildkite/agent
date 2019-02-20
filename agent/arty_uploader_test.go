package agent

import (
	"testing"
)

func TestParseArtifactoryDestinationBucketPath(t *testing.T) {
	for _, tc := range []struct {
		Destination, Path string
	}{
		{"rt://my-bucket-name/foo/bar", "foo/bar"},
		{"rt://stats-with-an-s/and-this-is-its/folder", "and-this-is-its/folder"},
	} {
		_, path := ParseArtifactoryDestination(tc.Destination)
		if path != tc.Path {
			t.Fatalf("Expected %q, got %q", tc.Path, path)
		}
	}
}

func TestParseArtifactoryDestinationBucketName(t *testing.T) {
	for _, tc := range []struct {
		Destination, Bucket string
	}{
		{"rt://my-bucket-name/foo/bar", "my-bucket-name"},
		{"rt://starts-with-an-s", "starts-with-an-s"},
	} {
		bucket, _ := ParseArtifactoryDestination(tc.Destination)
		if bucket != tc.Bucket {
			t.Fatalf("Expected %q, got %q", tc.Bucket, bucket)
		}
	}
}
