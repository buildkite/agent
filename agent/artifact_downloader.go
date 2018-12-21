package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/buildkite/agent/api"
	"github.com/buildkite/agent/logger"
	"github.com/buildkite/agent/pool"
)

type ArtifactDownloader struct {
	// The logger instance to use
	Logger logger.Logger

	// The APIClient that will be used when uploading jobs
	APIClient *api.Client

	// The ID of the Build
	BuildID string

	// The query used to find the artifacts
	Query string

	// Which step should we look at for the jobs
	Step string

	// Where we'll be downloading artifacts to
	Destination string
}

func (a *ArtifactDownloader) Download() error {
	// Turn the download destination into an absolute path and confirm it exists
	downloadDestination, _ := filepath.Abs(a.Destination)
	fileInfo, err := os.Stat(downloadDestination)
	if err != nil {
		return fmt.Errorf("Could not find information about destination: %s", downloadDestination)
	}
	if !fileInfo.IsDir() {
		return fmt.Errorf("%s is not a directory", downloadDestination)
	}

	// Find the artifacts that we want to download
	searcher := ArtifactSearcher{BuildID: a.BuildID, APIClient: a.APIClient}
	artifacts, err := searcher.Search(a.Query, a.Step)
	if err != nil {
		return err
	}

	artifactCount := len(artifacts)

	if artifactCount == 0 {
		return errors.New("No artifacts found for downloading")
	} else {
		a.Logger.Info("Found %d artifacts. Starting to download to: %s", artifactCount, downloadDestination)

		p := pool.New(pool.MaxConcurrencyLimit)
		errors := []error{}

		for _, artifact := range artifacts {
			// Create new instance of the artifact for the goroutine
			// See: http://golang.org/doc/effective_go.html#channels
			artifact := artifact

			p.Spawn(func() {
				var err error

				// Handle downloading from S3 and GS
				if strings.HasPrefix(artifact.UploadDestination, "s3://") {
					err = S3Downloader{
						Logger:      a.Logger,
						Path:        artifact.Path,
						Bucket:      artifact.UploadDestination,
						Destination: downloadDestination,
						Retries:     5,
						DebugHTTP:   a.APIClient.DebugHTTP,
					}.Start()
				} else if strings.HasPrefix(artifact.UploadDestination, "gs://") {
					err = GSDownloader{
						Logger:      a.Logger,
						Path:        artifact.Path,
						Bucket:      artifact.UploadDestination,
						Destination: downloadDestination,
						Retries:     5,
						DebugHTTP:   a.APIClient.DebugHTTP,
					}.Start()
				} else {
					err = Download{
						Logger:      a.Logger,
						URL:         artifact.URL,
						Path:        artifact.Path,
						Destination: downloadDestination,
						Retries:     5,
						DebugHTTP:   a.APIClient.DebugHTTP,
					}.Start()
				}

				// If the downloaded encountered an error, lock
				// the pool, collect it, then unlock the pool
				// again.
				if err != nil {
					a.Logger.Error("Failed to download artifact: %s", err)

					p.Lock()
					errors = append(errors, err)
					p.Unlock()
				}
			})
		}

		p.Wait()

		if len(errors) > 0 {
			return fmt.Errorf("There were errors with downloading some of the artifacts")
		}
	}

	return nil
}
