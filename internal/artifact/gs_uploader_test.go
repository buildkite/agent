package artifact

import (
	"testing"
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
