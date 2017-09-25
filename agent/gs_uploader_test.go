package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGSUploaderBucketPath(t *testing.T) {
	t.Parallel()

	gsUploader := GSUploader{Destination: "gs://my-bucket-name/foo/bar"}
	assert.Equal(t, gsUploader.BucketPath(), "foo/bar")

	gsUploader.Destination = "gs://starts-with-an-s/and-this-is-its/folder"
	assert.Equal(t, gsUploader.BucketPath(), "and-this-is-its/folder")
}

func TestGSUploaderBucketName(t *testing.T) {
	t.Parallel()

	gsUploader := GSUploader{Destination: "gs://my-bucket-name/foo/bar"}
	assert.Equal(t, gsUploader.BucketName(), "my-bucket-name")

	gsUploader.Destination = "gs://starts-with-an-s"
	assert.Equal(t, gsUploader.BucketName(), "starts-with-an-s")
}
