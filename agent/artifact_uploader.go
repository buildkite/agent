package agent

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/experiments"
	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/agent/v3/mime"
	"github.com/buildkite/agent/v3/pool"
	"github.com/buildkite/roko"
	"github.com/dustin/go-humanize"
	zglob "github.com/mattn/go-zglob"
)

const (
	ArtifactPathDelimiter    = ";"
	ArtifactFallbackMimeType = "binary/octet-stream"
)

type ArtifactUploaderConfig struct {
	// The ID of the Job
	JobID string

	// The path of the uploads
	Paths string

	// Where we'll be uploading artifacts
	Destination string

	// A specific Content-Type to use for all artifacts
	ContentType string

	// Whether to show HTTP debugging
	DebugHTTP bool

	// Whether to follow symbolic links when resolving globs
	FollowSymlinks bool
}

type ArtifactUploader struct {
	// The upload config
	conf ArtifactUploaderConfig

	// The logger instance to use
	logger logger.Logger

	// The APIClient that will be used when uploading jobs
	apiClient APIClient
}

func NewArtifactUploader(l logger.Logger, ac APIClient, c ArtifactUploaderConfig) *ArtifactUploader {
	return &ArtifactUploader{
		logger:    l,
		apiClient: ac,
		conf:      c,
	}
}

func (a *ArtifactUploader) Upload(ctx context.Context) error {
	// Create artifact structs for all the files we need to upload
	artifacts, err := a.Collect()
	if err != nil {
		return fmt.Errorf("collecting artifacts: %w", err)
	}

	if len(artifacts) == 0 {
		a.logger.Info("No files matched paths: %s", a.conf.Paths)
		return nil
	}

	a.logger.Info("Found %d files that match %q", len(artifacts), a.conf.Paths)
	if err := a.upload(ctx, artifacts); err != nil {
		return fmt.Errorf("uploading artifacts: %w", err)
	}

	return nil
}

func isDir(path string) bool {
	fi, err := os.Stat(path)
	if err != nil {
		return false
	}
	return fi.IsDir()
}

func (a *ArtifactUploader) Collect() (artifacts []*api.Artifact, err error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("getting working directory: %w", err)
	}

	// file paths are deduplicated after resolving globs etc
	seenPaths := make(map[string]bool)

	for _, globPath := range strings.Split(a.conf.Paths, ArtifactPathDelimiter) {
		globPath = strings.TrimSpace(globPath)
		if globPath == "" {
			continue
		}

		a.logger.Debug("Searching for %s", globPath)

		// Resolve the globs (with * and ** in them), if it's a non-globbed path and doesn't exists
		// then we will get the ErrNotExist that is handled below
		globfunc := zglob.Glob
		if a.conf.FollowSymlinks {
			// Follow symbolic links for files & directories while expanding globs
			globfunc = zglob.GlobFollowSymlinks
		}
		files, err := globfunc(globPath)
		if errors.Is(err, os.ErrNotExist) {
			a.logger.Info("File not found: %s", globPath)
			continue
		} else if err != nil {
			return nil, fmt.Errorf("resolving glob: %w", err)
		}

		// Process each glob match into an api.Artifact
		for _, file := range files {
			absolutePath, err := filepath.Abs(file)
			if err != nil {
				return nil, fmt.Errorf("resolving absolute path for file %s: %w", file, err)
			}

			// dedupe based on resolved absolutePath
			if _, ok := seenPaths[absolutePath]; ok {
				a.logger.Debug("Skipping duplicate path %s", file)
				continue
			}
			seenPaths[absolutePath] = true

			// Ignore directories, we only want files
			if isDir(absolutePath) {
				a.logger.Debug("Skipping directory %s", file)
				continue
			}

			// If a glob is absolute, we need to make it relative to the root so that
			// it can be combined with the download destination to make a valid path.
			// This is possibly weird and crazy, this logic dates back to
			// https://github.com/buildkite/agent/commit/8ae46d975aa60d1ae0e2cc0bff7a43d3bf960935
			// from 2014, so I'm replicating it here to avoid breaking things
			if filepath.IsAbs(globPath) {
				if runtime.GOOS == "windows" {
					wd = filepath.VolumeName(absolutePath) + "/"
				} else {
					wd = "/"
				}
			}

			path, err := filepath.Rel(wd, absolutePath)
			if err != nil {
				return nil, fmt.Errorf("resolving relative path for file %s: %w", file, err)
			}

			if experiments.IsEnabled("normalised-upload-paths") {
				// Convert any Windows paths to Unix/URI form
				path = filepath.ToSlash(path)
			}

			// Build an artifact object using the paths we have.
			artifact, err := a.build(path, absolutePath, globPath)
			if err != nil {
				return nil, fmt.Errorf("building artifact: %w", err)
			}

			artifacts = append(artifacts, artifact)
		}
	}

	return artifacts, nil
}

func (a *ArtifactUploader) build(path string, absolutePath string, globPath string) (*api.Artifact, error) {
	// Temporarily open the file to get its size
	file, err := os.Open(absolutePath)
	if err != nil {
		return nil, fmt.Errorf("opening file %s: %w", absolutePath, err)
	}
	defer file.Close()

	// Grab its file info (which includes its file size)
	fileInfo, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("getting file info for %s: %w", absolutePath, err)
	}

	// Generate a SHA-1 and SHA-256 checksums for the file
	hash1, hash256 := sha1.New(), sha256.New()
	io.Copy(io.MultiWriter(hash1, hash256), file)
	sha1sum := fmt.Sprintf("%040x", hash1.Sum(nil))
	sha256sum := fmt.Sprintf("%064x", hash256.Sum(nil))

	// Determine the Content-Type to send
	contentType := a.conf.ContentType

	if contentType == "" {
		extension := filepath.Ext(absolutePath)
		contentType = mime.TypeByExtension(extension)

		if contentType == "" {
			contentType = ArtifactFallbackMimeType
		}
	}

	// Create our new artifact data structure
	artifact := &api.Artifact{
		Path:         path,
		AbsolutePath: absolutePath,
		GlobPath:     globPath,
		FileSize:     fileInfo.Size(),
		Sha1Sum:      sha1sum,
		Sha256Sum:    sha256sum,
		ContentType:  contentType,
	}

	return artifact, nil
}

func (a *ArtifactUploader) upload(ctx context.Context, artifacts []*api.Artifact) error {
	var uploader Uploader
	var err error

	// Determine what uploader to use
	if a.conf.Destination != "" {
		if strings.HasPrefix(a.conf.Destination, "s3://") {
			uploader, err = NewS3Uploader(a.logger, S3UploaderConfig{
				Destination: a.conf.Destination,
				DebugHTTP:   a.conf.DebugHTTP,
			})
		} else if strings.HasPrefix(a.conf.Destination, "gs://") {
			uploader, err = NewGSUploader(a.logger, GSUploaderConfig{
				Destination: a.conf.Destination,
				DebugHTTP:   a.conf.DebugHTTP,
			})
		} else if strings.HasPrefix(a.conf.Destination, "rt://") {
			uploader, err = NewArtifactoryUploader(a.logger, ArtifactoryUploaderConfig{
				Destination: a.conf.Destination,
				DebugHTTP:   a.conf.DebugHTTP,
			})
		} else {
			return fmt.Errorf("invalid upload destination: '%v'. Only s3://, gs:// or rt:// upload schemes are allowed. Did you forget to surround your artifact upload pattern in double quotes?", a.conf.Destination)
		}

		a.logger.Info("Uploading to %q, using your agent configuration", a.conf.Destination)
	} else {
		uploader = NewFormUploader(a.logger, FormUploaderConfig{
			DebugHTTP: a.conf.DebugHTTP,
		})

		a.logger.Info("Uploading to default Buildkite artifact storage")
	}

	// Check if creation caused an error
	if err != nil {
		return fmt.Errorf("creating uploader: %v", err)
	}

	// Set the URLs of the artifacts based on the uploader
	for _, artifact := range artifacts {
		artifact.URL = uploader.URL(artifact)
	}

	// Create the artifacts on Buildkite
	batchCreator := NewArtifactBatchCreator(a.logger, a.apiClient, ArtifactBatchCreatorConfig{
		JobID:                  a.conf.JobID,
		Artifacts:              artifacts,
		UploadDestination:      a.conf.Destination,
		CreateArtifactsTimeout: 10 * time.Second,
	})

	artifacts, err = batchCreator.Create(ctx)
	if err != nil {
		return err
	}

	// Prepare a concurrency pool to upload the artifacts
	p := pool.New(pool.MaxConcurrencyLimit)
	errors := []error{}
	var errorsMutex sync.Mutex

	// Create a wait group so we can make sure the uploader waits for all
	// the artifact states to upload before finishing
	var stateUploaderWaitGroup sync.WaitGroup
	stateUploaderWaitGroup.Add(1)

	// A map to keep track of artifact states and how many we've uploaded
	artifactStates := make(map[string]string)
	artifactStatesUploaded := 0
	var artifactStatesMutex sync.Mutex

	// Spin up a gourtine that'll uploading artifact statuses every few
	// seconds in batches
	go func() {
		for artifactStatesUploaded < len(artifacts) {
			statesToUpload := make(map[string]string)

			// Grab all the states we need to upload, and remove
			// them from the tracking map
			//
			// Since we mutate the artifactStates variable in
			// multiple routines, we need to lock it to make sure
			// nothing else is changing it at the same time.
			artifactStatesMutex.Lock()
			for id, state := range artifactStates {
				statesToUpload[id] = state
				delete(artifactStates, id)
			}
			artifactStatesMutex.Unlock()

			if len(statesToUpload) > 0 {
				artifactStatesUploaded += len(statesToUpload)
				for id, state := range statesToUpload {
					a.logger.Debug("Artifact `%s` has state `%s`", id, state)
				}

				// Update the states of the artifacts in bulk.
				err := roko.NewRetrier(
					roko.WithMaxAttempts(10),
					roko.WithStrategy(roko.ExponentialSubsecond(500*time.Millisecond)),
				).DoWithContext(ctx, func(r *roko.Retrier) error {
					ctxShort, cancel := context.WithTimeout(ctx, 5*time.Second)
					defer cancel()
					if _, err := a.apiClient.UpdateArtifacts(ctxShort, a.conf.JobID, statesToUpload); err != nil {
						a.logger.Warn("%s (%s)", err, r)
						return err
					}
					return nil
				})
				if err != nil {
					a.logger.Error("Error uploading artifact states: %s", err)

					// Track the error that was raised. We need to
					// aquire a lock since we mutate the errors
					// slice in mutliple routines.
					errorsMutex.Lock()
					errors = append(errors, err)
					errorsMutex.Unlock()
				}

				a.logger.Debug("Uploaded %d artifact states (%d/%d)", len(statesToUpload), artifactStatesUploaded, len(artifacts))
			}

			// Check again for states to upload in a few seconds
			time.Sleep(1 * time.Second)
		}

		stateUploaderWaitGroup.Done()
	}()

	for _, artifact := range artifacts {
		// Create new instance of the artifact for the goroutine
		// See: http://golang.org/doc/effective_go.html#channels
		artifact := artifact

		p.Spawn(func() {
			// Show a nice message that we're starting to upload the file
			a.logger.Info("Uploading artifact %s %s (%s)", artifact.ID, artifact.Path, humanize.Bytes(uint64(artifact.FileSize)))

			var state string

			// Upload the artifact and then set the state depending
			// on whether or not it passed. We'll retry the upload
			// a couple of times before giving up.
			err := roko.NewRetrier(
				roko.WithMaxAttempts(10),
				roko.WithStrategy(roko.Constant(5*time.Second)),
			).DoWithContext(ctx, func(r *roko.Retrier) error {
				if err := uploader.Upload(artifact); err != nil {
					a.logger.Warn("%s (%s)", err, r)
					return err
				}
				return nil
			})
			// Did the upload eventually fail?
			if err != nil {
				a.logger.Error("Error uploading artifact \"%s\": %s", artifact.Path, err)

				// Track the error that was raised. We need to
				// acquire a lock since we mutate the errors
				// slice in multiple routines.
				errorsMutex.Lock()
				errors = append(errors, err)
				errorsMutex.Unlock()

				state = "error"
			} else {
				a.logger.Info("Successfully uploaded artifact \"%s\"", artifact.Path)
				state = "finished"
			}

			// Since we mutate the artifactStates variable in
			// multiple routines, we need to lock it to make sure
			// nothing else is changing it at the same time.
			artifactStatesMutex.Lock()
			artifactStates[artifact.ID] = state
			artifactStatesMutex.Unlock()
		})
	}

	a.logger.Debug("Waiting for uploads to complete...")

	// Wait for the pool to finish
	p.Wait()

	a.logger.Debug("Uploads complete, waiting for upload status to be sent to buildkite...")

	// Wait for the statuses to finish uploading
	stateUploaderWaitGroup.Wait()

	if len(errors) > 0 {
		return fmt.Errorf("errors uploading artifacts: %v", errors)
	}

	a.logger.Info("Artifact uploads completed successfully")

	return nil
}
