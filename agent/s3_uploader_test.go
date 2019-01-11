package agent

import (
	"testing"

	"github.com/buildkite/agent/logger"
	"github.com/stretchr/testify/assert"
)

func TestS3UploaderBucketPath(t *testing.T) {
	s3Uploader, err := NewS3Uploader(logger.Discard, S3UploaderConfig{
		Destination: "s3://my-bucket-name/foo/bar",
	})
	assert.NoError(t, err)
	assert.Equal(t, s3Uploader.BucketPath, "foo/bar")

	s3Uploader, err = NewS3Uploader(logger.Discard, S3UploaderConfig{
		Destination: "s3://starts-with-an-s/and-this-is-its/folder",
	})
	assert.NoError(t, err)
	assert.Equal(t, s3Uploader.BucketPath, "and-this-is-its/folder")
}

func TestS3UploaderBucketName(t *testing.T) {
	s3Uploader, err := NewS3Uploader(logger.Discard, S3UploaderConfig{
		Destination: "s3://my-bucket-name/foo/bar",
	})
	assert.NoError(t, err)
	assert.Equal(t, s3Uploader.BucketName, "my-bucket-name")

	s3Uploader, err = NewS3Uploader(logger.Discard, S3UploaderConfig{
		Destination: "s3://starts-with-an-s",
	})
	assert.NoError(t, err)
	assert.Equal(t, s3Uploader.BucketName, "starts-with-an-s")
}
