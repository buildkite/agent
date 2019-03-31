package agent

import (
	"testing"

	"github.com/buildkite/agent/logger"
	"gotest.tools/assert"
	is "gotest.tools/assert/cmp"
)

func TestArtifactoryDowloaderRepositoryPath(t *testing.T) {
	t.Parallel()

	rtUploader := NewArtifactoryDownloader(logger.Discard, ArtifactoryDownloaderConfig{
		Repository: "rt://my-bucket-name/foo/bar",
	})
	assert.Check(t, is.Equal(rtUploader.RepositoryPath(), "foo/bar"))

	rtUploader = NewArtifactoryDownloader(logger.Discard, ArtifactoryDownloaderConfig{
		Repository: "rt://starts-with-an-s/and-this-is-its/folder",
	})
	assert.Check(t, is.Equal(rtUploader.RepositoryPath(), "and-this-is-its/folder"))
}

func TestArtifactoryDowloaderRepositoryName(t *testing.T) {
	t.Parallel()

	rtUploader := NewArtifactoryDownloader(logger.Discard, ArtifactoryDownloaderConfig{
		Repository: "rt://my-bucket-name/foo/bar",
	})
	assert.Check(t, is.Equal(rtUploader.RepositoryName(), "my-bucket-name"))

	rtUploader = NewArtifactoryDownloader(logger.Discard, ArtifactoryDownloaderConfig{
		Repository: "rt://starts-with-an-s",
	})
	assert.Check(t, is.Equal(rtUploader.RepositoryName(), "starts-with-an-s"))
}

func TestArtifactoryDowloaderRepositoryFileLocation(t *testing.T) {
	t.Parallel()

	rtUploader := NewArtifactoryDownloader(logger.Discard, ArtifactoryDownloaderConfig{
		Repository: "rt://my-bucket-name/rt/folder",
		Path:       "here/please/right/now/",
	})
	assert.Check(t, is.Equal(rtUploader.RepositoryFileLocation(), "rt/folder/here/please/right/now/"))

	rtUploader = NewArtifactoryDownloader(logger.Discard, ArtifactoryDownloaderConfig{
		Repository: "rt://my-bucket-name/rt/folder",
		Path:       "",
	})
	assert.Check(t, is.Equal(rtUploader.RepositoryFileLocation(), "rt/folder/"))
}
