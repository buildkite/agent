package artifact

import (
	"testing"

	"github.com/buildkite/agent/v3/logger"
	"github.com/stretchr/testify/assert"
)

func TestS3DowloaderBucketPath(t *testing.T) {
	t.Parallel()

	s3Downloader := NewS3Downloader(logger.Discard, S3DownloaderConfig{
		S3Path: "s3://my-bucket-name/foo/bar",
	})
	assert.Equal(t, s3Downloader.BucketPath(), "foo/bar")

	s3Downloader = NewS3Downloader(logger.Discard, S3DownloaderConfig{
		S3Path: "s3://starts-with-an-s/and-this-is-its/folder",
	})
	assert.Equal(t, s3Downloader.BucketPath(), "and-this-is-its/folder")
}

func TestS3DowloaderBucketName(t *testing.T) {
	t.Parallel()

	s3Downloader := NewS3Downloader(logger.Discard, S3DownloaderConfig{
		S3Path: "s3://my-bucket-name/foo/bar",
	})
	assert.Equal(t, s3Downloader.BucketName(), "my-bucket-name")

	s3Downloader = NewS3Downloader(logger.Discard, S3DownloaderConfig{
		S3Path: "s3://starts-with-an-s",
	})
	assert.Equal(t, s3Downloader.BucketName(), "starts-with-an-s")
}

func TestS3DowloaderBucketFileLocation(t *testing.T) {
	t.Parallel()

	s3Downloader := NewS3Downloader(logger.Discard, S3DownloaderConfig{
		S3Path: "s3://my-bucket-name/s3/folder",
		Path:   "here/please/right/now/",
	})
	assert.Equal(t, s3Downloader.BucketFileLocation(), "s3/folder/here/please/right/now/")

	s3Downloader = NewS3Downloader(logger.Discard, S3DownloaderConfig{
		S3Path: "s3://my-bucket-name/s3/folder",
		Path:   "",
	})
	assert.Equal(t, s3Downloader.BucketFileLocation(), "s3/folder/")
}
