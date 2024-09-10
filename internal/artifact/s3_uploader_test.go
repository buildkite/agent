package artifact

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseS3Destination(t *testing.T) {
	for _, tc := range []struct {
		dest, bucket, path string
	}{
		{
			dest:   "s3://my-bucket-name/foo/bar",
			bucket: "my-bucket-name",
			path:   "foo/bar",
		},
		{
			dest:   "s3://starts-with-an-s/and-this-is-its/folder",
			bucket: "starts-with-an-s",
			path:   "and-this-is-its/folder",
		},
		{
			dest:   "s3://custom-s3-domain/folder/ends-with-a-slash/",
			bucket: "custom-s3-domain",
			path:   "folder/ends-with-a-slash",
		},
	} {
		bucket, path := ParseS3Destination(tc.dest)
		if bucket != tc.bucket || path != tc.path {
			t.Errorf("ParseS3Destination(%q) = (%q, %q), want (%q, %q)", tc.dest, bucket, path, tc.bucket, tc.path)
		}
	}
}

func TestResolveServerSideEncryptionConfig(t *testing.T) {

	assert := require.New(t)

	for _, tc := range []struct {
		ServerSideEncryptionConfig string
		ExpectedResult             bool
	}{
		{"True", true},
		{"falsE", false},
		{"lol", false},
	} {
		uploader := &S3Uploader{}
		os.Setenv("BUILDKITE_S3_SSE_ENABLED", tc.ServerSideEncryptionConfig)
		config := uploader.serverSideEncryptionEnabled()

		assert.Equal(tc.ExpectedResult, config)

		os.Unsetenv("BUILDKITE_S3_SSE_ENABLED")
	}
}

func TestResolvePermission(t *testing.T) {

	assert := require.New(t)
	for _, tc := range []struct {
		Permission     string
		ExpectedResult string
		ShouldErr      bool
	}{
		{"", "public-read", false},
		{"private", "private", false},
		{"public-read", "public-read", false},
		{"public-read-write", "public-read-write", false},
		{"authenticated-read", "authenticated-read", false},
		{"bucket-owner-read", "bucket-owner-read", false},
		{"bucket-owner-full-control", "bucket-owner-full-control", false},
		{"foo", "", true},
	} {
		uploader := &S3Uploader{}
		os.Setenv("BUILDKITE_S3_ACL", tc.Permission)
		config, err := uploader.resolvePermission()

		// if it should error we just look at the error
		if tc.ShouldErr {
			assert.Error(err)
		} else {
			assert.Nil(err)
			assert.Equal(tc.ExpectedResult, config)
		}

		os.Unsetenv("BUILDKITE_S3_ACL")
	}
}
