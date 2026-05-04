package artifact

import (
	"testing"

	"github.com/buildkite/agent/v4/logger"
)

func TestArtifactoryDownloaderRepositoryPath(t *testing.T) {
	t.Parallel()

	rtUploader := NewArtifactoryDownloader(logger.SlogDiscard, ArtifactoryDownloaderConfig{
		Repository: "rt://my-bucket-name/foo/bar",
	})
	if got, want := rtUploader.RepositoryPath(), "foo/bar"; got != want {
		t.Errorf("rtUploader.RepositoryPath() = %q, want %q", got, want)
	}

	rtUploader = NewArtifactoryDownloader(logger.SlogDiscard, ArtifactoryDownloaderConfig{
		Repository: "rt://starts-with-an-s/and-this-is-its/folder",
	})
	if got, want := rtUploader.RepositoryPath(), "and-this-is-its/folder"; got != want {
		t.Errorf("rtUploader.RepositoryPath() = %q, want %q", got, want)
	}
}

func TestArtifactoryDownloaderRepositoryName(t *testing.T) {
	t.Parallel()

	rtUploader := NewArtifactoryDownloader(logger.SlogDiscard, ArtifactoryDownloaderConfig{
		Repository: "rt://my-bucket-name/foo/bar",
	})
	if got, want := rtUploader.RepositoryName(), "my-bucket-name"; got != want {
		t.Errorf("rtUploader.RepositoryName() = %q, want %q", got, want)
	}

	rtUploader = NewArtifactoryDownloader(logger.SlogDiscard, ArtifactoryDownloaderConfig{
		Repository: "rt://starts-with-an-s",
	})
	if got, want := rtUploader.RepositoryName(), "starts-with-an-s"; got != want {
		t.Errorf("rtUploader.RepositoryName() = %q, want %q", got, want)
	}
}

func TestArtifactoryDownloaderRepositoryFileLocation(t *testing.T) {
	t.Parallel()

	rtUploader := NewArtifactoryDownloader(logger.SlogDiscard, ArtifactoryDownloaderConfig{
		Repository: "rt://my-bucket-name/rt/folder",
		Path:       "here/please/right/now/",
	})
	if got, want := rtUploader.RepositoryFileLocation(), "rt/folder/here/please/right/now"; got != want {
		t.Errorf("rtUploader.RepositoryFileLocation() = %q, want %q", got, want)
	}

	rtUploader = NewArtifactoryDownloader(logger.SlogDiscard, ArtifactoryDownloaderConfig{
		Repository: "rt://my-bucket-name/rt/folder",
		Path:       "",
	})
	if got, want := rtUploader.RepositoryFileLocation(), "rt/folder"; got != want {
		t.Errorf("rtUploader.RepositoryFileLocation() = %q, want %q", got, want)
	}
}
