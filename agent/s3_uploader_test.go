package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBucketPath(t *testing.T) {
	s3Uploader := S3Uploader{Destination: "s3://my-bucket-name/foo/bar"}
	assert.Equal(t, s3Uploader.BucketPath(), "foo/bar")

	s3Uploader.Destination = "s3://starts-with-an-s/and-this-is-its/folder"
	assert.Equal(t, s3Uploader.BucketPath(), "and-this-is-its/folder")
}

func TestBucketName(t *testing.T) {
	s3Uploader := S3Uploader{Destination: "s3://my-bucket-name/foo/bar"}
	assert.Equal(t, s3Uploader.BucketName(), "my-bucket-name")

	s3Uploader.Destination = "s3://starts-with-an-s"
	assert.Equal(t, s3Uploader.BucketName(), "starts-with-an-s")
}
