package agent

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
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
