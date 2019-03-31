package agent

import (
	"testing"

	"github.com/buildkite/agent/logger"
	"gotest.tools/assert"
	is "gotest.tools/assert/cmp"
)

func TestS3DowloaderBucketPath(t *testing.T) {
	t.Parallel()

	s3Uploader := NewS3Downloader(logger.Discard, S3DownloaderConfig{
		Bucket: "s3://my-bucket-name/foo/bar",
	})
	assert.Check(t, is.Equal(s3Uploader.BucketPath(), "foo/bar"))

	s3Uploader = NewS3Downloader(logger.Discard, S3DownloaderConfig{
		Bucket: "s3://starts-with-an-s/and-this-is-its/folder",
	})
	assert.Check(t, is.Equal(s3Uploader.BucketPath(), "and-this-is-its/folder"))
}

func TestS3DowloaderBucketName(t *testing.T) {
	t.Parallel()

	s3Uploader := NewS3Downloader(logger.Discard, S3DownloaderConfig{
		Bucket: "s3://my-bucket-name/foo/bar",
	})
	assert.Check(t, is.Equal(s3Uploader.BucketName(), "my-bucket-name"))

	s3Uploader = NewS3Downloader(logger.Discard, S3DownloaderConfig{
		Bucket: "s3://starts-with-an-s",
	})
	assert.Check(t, is.Equal(s3Uploader.BucketName(), "starts-with-an-s"))
}

func TestS3DowloaderBucketFileLocation(t *testing.T) {
	t.Parallel()

	s3Uploader := NewS3Downloader(logger.Discard, S3DownloaderConfig{
		Bucket: "s3://my-bucket-name/s3/folder",
		Path:   "here/please/right/now/",
	})
	assert.Check(t, is.Equal(s3Uploader.BucketFileLocation(), "s3/folder/here/please/right/now/"))

	s3Uploader = NewS3Downloader(logger.Discard, S3DownloaderConfig{
		Bucket: "s3://my-bucket-name/s3/folder",
		Path:   "",
	})
	assert.Check(t, is.Equal(s3Uploader.BucketFileLocation(), "s3/folder/"))
}
