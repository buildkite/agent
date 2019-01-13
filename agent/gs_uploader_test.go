package agent

import (
	"testing"
)

func TestParseGSDestinationBucketPath(t *testing.T) {
	for _, tc := range []struct {
		Destination, Path string
	}{
		{"gs://my-bucket-name/foo/bar", "foo/bar"},
		{"gs://starts-with-an-s/and-this-is-its/folder", "and-this-is-its/folder"},
	} {
		_, path := ParseGSDestination(tc.Destination)
		if path != tc.Path {
			t.Fatalf("Expected %q, got %q", tc.Path, path)
		}
	}
}

func TestParseGSDestinationBucketName(t *testing.T) {
	for _, tc := range []struct {
		Destination, Bucket string
	}{
		{"gs://my-bucket-name/foo/bar", "my-bucket-name"},
		{"gs://starts-with-an-s", "starts-with-an-s"},
	} {
		bucket, _ := ParseGSDestination(tc.Destination)
		if bucket != tc.Bucket {
			t.Fatalf("Expected %q, got %q", tc.Bucket, bucket)
		}
	}
}
