package agent

import (
	"testing"
)

func TestParseS3DestinationBucketPath(t *testing.T) {
	for _, tc := range []struct {
		Destination, Path string
	}{
		{"s3://my-bucket-name/foo/bar", "foo/bar"},
		{"s3://starts-with-an-s/and-this-is-its/folder", "and-this-is-its/folder"},
	} {
		_, path := ParseS3Destination(tc.Destination)
		if path != tc.Path {
			t.Fatalf("Expected %q, got %q", tc.Path, path)
		}
	}
}

func TestParseS3DestinationBucketName(t *testing.T) {
	for _, tc := range []struct {
		Destination, Bucket string
	}{
		{"s3://my-bucket-name/foo/bar", "my-bucket-name"},
		{"s3://starts-with-an-s", "starts-with-an-s"},
	} {
		bucket, _ := ParseS3Destination(tc.Destination)
		if bucket != tc.Bucket {
			t.Fatalf("Expected %q, got %q", tc.Bucket, bucket)
		}
	}
}
