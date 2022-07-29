package agent

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/agent/v3/pool"
)

type ArtifactDownloaderConfig struct {
	// The ID of the Build
	BuildID string

	// The query used to find the artifacts
	Query string

	// Which step should we look at for the jobs
	Step string

	// Whether to include artifacts from retried jobs in the search
	IncludeRetriedJobs bool

	// Where we'll be downloading artifacts to
	Destination string

	// Whether to show HTTP debugging
	DebugHTTP bool
}

type ArtifactDownloader struct {
	// The config for downloading
	conf ArtifactDownloaderConfig

	// The logger instance to use
	logger logger.Logger

	// The APIClient that will be used when uploading jobs
	apiClient APIClient
}

func NewArtifactDownloader(l logger.Logger, ac APIClient, c ArtifactDownloaderConfig) ArtifactDownloader {
	return ArtifactDownloader{
		logger:    l,
		apiClient: ac,
		conf:      c,
	}
}

func (a *ArtifactDownloader) Download() error {
	// Turn the download destination into an absolute path and confirm it exists
	downloadDestination, _ := filepath.Abs(a.conf.Destination)
	fileInfo, err := os.Stat(downloadDestination)
	if err != nil {
		return fmt.Errorf("Could not find information about destination: %s %v",
			downloadDestination, err)
	}
	if !fileInfo.IsDir() {
		return fmt.Errorf("%s is not a directory", downloadDestination)
	}

	// Find the artifacts that we want to download
	state := "finished"
	artifacts, err := NewArtifactSearcher(a.logger, a.apiClient, a.conf.BuildID).
		Search(a.conf.Query, a.conf.Step, state, a.conf.IncludeRetriedJobs, false)
	if err != nil {
		return err
	}

	artifactCount := len(artifacts)

	if artifactCount == 0 {
		return errors.New("No artifacts found for downloading")
	}

	a.logger.Info("Found %d artifacts. Starting to download to: %s", artifactCount, downloadDestination)

	p := pool.New(pool.MaxConcurrencyLimit)
	errors := []error{}
	s3Clients := map[string]*s3.S3{}

	for _, artifact := range artifacts {
		// Create new instance of the artifact for the goroutine
		// See: http://golang.org/doc/effective_go.html#channels
		artifact := artifact

		p.Spawn(func() {
			var err error
			var path string = artifact.Path

			// Convert windows paths to slashes, otherwise we get a literal
			// download of "dir/dir/file" vs sub-directories on non-windows agents
			if runtime.GOOS != `windows` {
				path = strings.Replace(path, `\`, `/`, -1)
			}

			// Handle downloading from S3, GS, or RT
			if strings.HasPrefix(artifact.UploadDestination, "s3://") {
				// We want to have as few S3 clients as possible, as creating them is kind of an expensive operation
				// But it's also theoretically possible that we'll have multiple artifacts with different S3 buckets, and each
				// S3Client only applies to one bucket, so we need to store the S3 clients in a map, one for each bucket
				bucketName, _ := ParseS3Destination(artifact.UploadDestination)
				if _, has := s3Clients[bucketName]; !has {
					p.Lock()
					client, err := NewS3Client(a.logger, bucketName)
					if err != nil {
						err = fmt.Errorf("Failed to create S3 client for bucket %s: %w", bucketName, err)
						a.logger.Error("%v", err)

						errors = append(errors, err)
						p.Unlock()
						return
					}

					s3Clients[bucketName] = client
					p.Unlock()
				}

				err = NewS3Downloader(a.logger, S3DownloaderConfig{
					S3Client:    s3Clients[artifact.UploadDestination],
					Path:        path,
					S3Path:      artifact.UploadDestination,
					Destination: downloadDestination,
					Retries:     5,
					DebugHTTP:   a.conf.DebugHTTP,
				}).Start()
			} else if strings.HasPrefix(artifact.UploadDestination, "gs://") {
				err = NewGSDownloader(a.logger, GSDownloaderConfig{
					Path:        path,
					Bucket:      artifact.UploadDestination,
					Destination: downloadDestination,
					Retries:     5,
					DebugHTTP:   a.conf.DebugHTTP,
				}).Start()
			} else if strings.HasPrefix(artifact.UploadDestination, "rt://") {
				err = NewArtifactoryDownloader(a.logger, ArtifactoryDownloaderConfig{
					Path:        path,
					Repository:  artifact.UploadDestination,
					Destination: downloadDestination,
					Retries:     5,
					DebugHTTP:   a.conf.DebugHTTP,
				}).Start()
			} else {
				err = NewDownload(a.logger, http.DefaultClient, DownloadConfig{
					URL:         artifact.URL,
					Path:        path,
					Destination: downloadDestination,
					Retries:     5,
					DebugHTTP:   a.conf.DebugHTTP,
				}).Start()
			}

			// If the downloaded encountered an error, lock
			// the pool, collect it, then unlock the pool
			// again.
			if err != nil {
				a.logger.Error("Failed to download artifact: %s", err)

				p.Lock()
				errors = append(errors, err)
				p.Unlock()
			}
		})

		p.Wait()

		if len(errors) > 0 {
			return fmt.Errorf("There were errors with downloading some of the artifacts")
		}
	}

	return nil
}
