package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBucketPath(t *testing.T) {
	s3Uploader := S3Uploader{Destination: "s3://my-bucket-name/foo/bar"}

	assert.Equal(t, s3Uploader.BucketPath(), "foo/bar")
}

func TestBucketName(t *testing.T) {
	s3Uploader := S3Uploader{Destination: "s3://my-bucket-name/foo/bar"}

	assert.Equal(t, s3Uploader.BucketName(), "my-bucket-name")
}
