package artifact

import (
	"testing"

	"github.com/buildkite/agent/v4/logger"
)

func TestS3DowloaderBucketPath(t *testing.T) {
	t.Parallel()

	s3Downloader := NewS3Downloader(logger.Discard, S3DownloaderConfig{
		S3Path: "s3://my-bucket-name/foo/bar",
	})
	if got, want := s3Downloader.BucketPath(), "foo/bar"; got != want {
		t.Errorf("s3Downloader.BucketPath() = %q, want %q", got, want)
	}

	s3Downloader = NewS3Downloader(logger.Discard, S3DownloaderConfig{
		S3Path: "s3://starts-with-an-s/and-this-is-its/folder",
	})
	if got, want := s3Downloader.BucketPath(), "and-this-is-its/folder"; got != want {
		t.Errorf("s3Downloader.BucketPath() = %q, want %q", got, want)
	}
}

func TestS3DowloaderBucketName(t *testing.T) {
	t.Parallel()

	s3Downloader := NewS3Downloader(logger.Discard, S3DownloaderConfig{
		S3Path: "s3://my-bucket-name/foo/bar",
	})
	if got, want := s3Downloader.BucketName(), "my-bucket-name"; got != want {
		t.Errorf("s3Downloader.BucketName() = %q, want %q", got, want)
	}

	s3Downloader = NewS3Downloader(logger.Discard, S3DownloaderConfig{
		S3Path: "s3://starts-with-an-s",
	})
	if got, want := s3Downloader.BucketName(), "starts-with-an-s"; got != want {
		t.Errorf("s3Downloader.BucketName() = %q, want %q", got, want)
	}
}

func TestS3DowloaderBucketFileLocation(t *testing.T) {
	t.Parallel()

	s3Downloader := NewS3Downloader(logger.Discard, S3DownloaderConfig{
		S3Path: "s3://my-bucket-name/s3/folder",
		Path:   "here/please/right/now/",
	})
	if got, want := s3Downloader.BucketFileLocation(), "s3/folder/here/please/right/now/"; got != want {
		t.Errorf("s3Downloader.BucketFileLocation() = %q, want %q", got, want)
	}

	s3Downloader = NewS3Downloader(logger.Discard, S3DownloaderConfig{
		S3Path: "s3://my-bucket-name/s3/folder",
		Path:   "",
	})
	if got, want := s3Downloader.BucketFileLocation(), "s3/folder/"; got != want {
		t.Errorf("s3Downloader.BucketFileLocation() = %q, want %q", got, want)
	}
}
