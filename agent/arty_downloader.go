package agent

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/buildkite/agent/v3/logger"
)

type ArtifactoryDownloaderConfig struct {
	// The Artifactory repository name and the path, e.g rt://my-repo-name/foo/bar
	Repository string

	// The root directory of the download
	Destination string

	// The relative path that should be preserved in the download folder,
	// also its location in the repo
	Path string

	// How many times should it retry the download before giving up
	Retries int

	// If failed responses should be dumped to the log
	DebugHTTP bool
}

type ArtifactoryDownloader struct {
	// The download config
	conf ArtifactoryDownloaderConfig

	// The logger instance to use
	logger logger.Logger
}

func NewArtifactoryDownloader(l logger.Logger, c ArtifactoryDownloaderConfig) *ArtifactoryDownloader {
	return &ArtifactoryDownloader{
		conf:   c,
		logger: l,
	}
}

func (d ArtifactoryDownloader) Start() error {
	// Pull environment variables
	stringURL := os.Getenv("BUILDKITE_ARTIFACTORY_URL")
	username := os.Getenv("BUILDKITE_ARTIFACTORY_USER")
	password := os.Getenv("BUILDKITE_ARTIFACTORY_PASSWORD")
	if stringURL == "" || username == "" || password == "" {
		return errors.New("Must set BUILDKITE_ARTIFACTORY_URL, BUILDKITE_ARTIFACTORY_USER, BUILDKITE_ARTIFACTORY_PASSWORD when using rt:// path")
	}

	// create full URL
	fullURL := fmt.Sprintf("%s/%s/%s",
		strings.TrimSuffix(stringURL, "/"),
		d.RepositoryName(),
		d.RepositoryFileLocation(),
	)

	// create headers map
	headers := map[string]string{
		"Authorization": fmt.Sprintf("Basic %s", getBasicAuthHeader(username, password)),
	}

	// We can now cheat and pass the URL onto our regular downloader
	return NewDownload(d.logger, http.DefaultClient, DownloadConfig{
		URL:         fullURL,
		Path:        d.conf.Path,
		Destination: d.conf.Destination,
		Retries:     d.conf.Retries,
		Headers:     headers,
		DebugHTTP:   d.conf.DebugHTTP,
	}).Start()
}

func (d ArtifactoryDownloader) RepositoryFileLocation() string {
	if d.RepositoryPath() != "" {
		return path.Join(strings.TrimSuffix(d.RepositoryPath(), "/"), "/", strings.TrimPrefix(filepath.ToSlash(d.conf.Path), "/"))
	} else {
		return d.conf.Path
	}
}

func (d ArtifactoryDownloader) RepositoryPath() string {
	return strings.Join(d.destinationParts()[1:len(d.destinationParts())], "/")
}

func (d ArtifactoryDownloader) RepositoryName() string {
	return d.destinationParts()[0]
}

func (d ArtifactoryDownloader) destinationParts() []string {
	trimmed := strings.TrimPrefix(d.conf.Repository, "rt://")

	return strings.Split(trimmed, "/")
}

func getBasicAuthHeader(username, password string) string {
	auth := username + ":" + password
	return base64.StdEncoding.EncodeToString([]byte(auth))
}
