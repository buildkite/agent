package agent

import (
	"testing"

	"github.com/buildkite/agent/logger"
	"github.com/stretchr/testify/assert"
)

func TestGSUploaderBucketPath(t *testing.T) {
	t.Parallel()

	gsUploader, err := NewGSUploader(logger.Discard, GSUploaderConfig{
		Destination: "gs://my-bucket-name/foo/bar",
	})
	assert.NoError(t, err)
	assert.Equal(t, gsUploader.BucketPath(), "foo/bar")

	gsUploader, err = NewGSUploader(logger.Discard, GSUploaderConfig{
		Destination: "gs://starts-with-an-s/and-this-is-its/folder",
	})

	assert.NoError(t, err)
	assert.Equal(t, gsUploader.BucketPath(), "and-this-is-its/folder")
}

func TestGSUploaderBucketName(t *testing.T) {
	t.Parallel()

	gsUploader, err := NewGSUploader(logger.Discard, GSUploaderConfig{
		Destination: "gs://my-bucket-name/foo/bar",
	})
	assert.NoError(t, err)
	assert.Equal(t, gsUploader.BucketName(), "my-bucket-name")

	gsUploader, err = NewGSUploader(logger.Discard, GSUploaderConfig{
		Destination: "gs://starts-with-an-s",
	})
	assert.NoError(t, err)
	assert.Equal(t, gsUploader.BucketName(), "starts-with-an-s")
}
