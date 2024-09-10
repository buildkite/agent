package artifact

import (
	"testing"

	"github.com/buildkite/agent/v3/logger"
	"github.com/stretchr/testify/assert"
)

func TestArtifactoryDownloaderRepositoryPath(t *testing.T) {
	t.Parallel()

	rtUploader := NewArtifactoryDownloader(logger.Discard, ArtifactoryDownloaderConfig{
		Repository: "rt://my-bucket-name/foo/bar",
	})
	assert.Equal(t, rtUploader.RepositoryPath(), "foo/bar")

	rtUploader = NewArtifactoryDownloader(logger.Discard, ArtifactoryDownloaderConfig{
		Repository: "rt://starts-with-an-s/and-this-is-its/folder",
	})
	assert.Equal(t, rtUploader.RepositoryPath(), "and-this-is-its/folder")
}

func TestArtifactoryDownloaderRepositoryName(t *testing.T) {
	t.Parallel()

	rtUploader := NewArtifactoryDownloader(logger.Discard, ArtifactoryDownloaderConfig{
		Repository: "rt://my-bucket-name/foo/bar",
	})
	assert.Equal(t, rtUploader.RepositoryName(), "my-bucket-name")

	rtUploader = NewArtifactoryDownloader(logger.Discard, ArtifactoryDownloaderConfig{
		Repository: "rt://starts-with-an-s",
	})
	assert.Equal(t, rtUploader.RepositoryName(), "starts-with-an-s")
}

func TestArtifactoryDownloaderRepositoryFileLocation(t *testing.T) {
	t.Parallel()

	rtUploader := NewArtifactoryDownloader(logger.Discard, ArtifactoryDownloaderConfig{
		Repository: "rt://my-bucket-name/rt/folder",
		Path:       "here/please/right/now/",
	})
	assert.Equal(t, rtUploader.RepositoryFileLocation(), "rt/folder/here/please/right/now")

	rtUploader = NewArtifactoryDownloader(logger.Discard, ArtifactoryDownloaderConfig{
		Repository: "rt://my-bucket-name/rt/folder",
		Path:       "",
	})
	assert.Equal(t, rtUploader.RepositoryFileLocation(), "rt/folder")
}
