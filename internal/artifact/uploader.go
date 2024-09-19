package artifact

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/DrJosh9000/zzglob"
	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/internal/experiments"
	"github.com/buildkite/agent/v3/internal/mime"
	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/roko"
	"github.com/dustin/go-humanize"
	"github.com/mattn/go-zglob"
)

const (
	ArtifactPathDelimiter    = ";"
	ArtifactFallbackMimeType = "binary/octet-stream"
)

type UploaderConfig struct {
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
	GlobResolveFollowSymlinks bool

	// Whether to not upload symlinks
	UploadSkipSymlinks bool
}

type Uploader struct {
	// The upload config
	conf UploaderConfig

	// The logger instance to use
	logger logger.Logger

	// The APIClient that will be used when uploading jobs
	apiClient APIClient
}

func NewUploader(l logger.Logger, ac APIClient, c UploaderConfig) *Uploader {
	return &Uploader{
		logger:    l,
		apiClient: ac,
		conf:      c,
	}
}

func (a *Uploader) Upload(ctx context.Context) error {
	// Create artifact structs for all the files we need to upload
	artifacts, err := a.collect(ctx)
	if err != nil {
		return fmt.Errorf("collecting artifacts: %w", err)
	}

	if len(artifacts) == 0 {
		a.logger.Info("No files matched paths: %s", a.conf.Paths)
		return nil
	}

	a.logger.Info("Found %d files that match %q", len(artifacts), a.conf.Paths)

	// Determine what uploader to use
	uploader, err := a.createUploader()
	if err != nil {
		return fmt.Errorf("creating uploader: %w", err)
	}

	// Set the URLs of the artifacts based on the uploader
	for _, artifact := range artifacts {
		artifact.URL = uploader.URL(artifact)
	}

	// Batch-create the artifact records on Buildkite
	batchCreator := NewArtifactBatchCreator(a.logger, a.apiClient, BatchCreatorConfig{
		JobID:                  a.conf.JobID,
		Artifacts:              artifacts,
		UploadDestination:      a.conf.Destination,
		CreateArtifactsTimeout: 10 * time.Second,
	})
	artifacts, err = batchCreator.Create(ctx)
	if err != nil {
		return err
	}

	if err := a.upload(ctx, artifacts, uploader); err != nil {
		return fmt.Errorf("uploading artifacts: %w", err)
	}

	return nil
}

func isSymlink(path string) bool {
	fi, err := os.Lstat(path)
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeSymlink != 0
}

func isDir(path string) bool {
	fi, err := os.Stat(path)
	if err != nil {
		return false
	}
	return fi.IsDir()
}

func (a *Uploader) collect(ctx context.Context) ([]*api.Artifact, error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("getting working directory: %w", err)
	}

	ac := &artifactCollector{
		Uploader:  a,
		wd:        wd,
		seenPaths: make(map[string]bool),
	}

	filesCh := make(chan string)

	// Create a few workers to process files as they are found.
	// Because a single glob could match many many files, a fixed number of
	// workers will avoid slamming the runtime (as could happen with a
	// goroutine per file).
	wctx, cancel := context.WithCancelCause(ctx)
	var wg sync.WaitGroup
	for range runtime.GOMAXPROCS(0) {
		wg.Add(1)
		go func() {
			defer wg.Done()

			if err := ac.worker(wctx, filesCh); err != nil {
				cancel(err)
			}
		}()
	}

	// Start resolving globs into files.
	if err := a.glob(wctx, filesCh); err != nil {
		cancel(err)
	}

	wg.Wait()

	if err := context.Cause(wctx); err != nil {
		return nil, err
	}

	return ac.artifacts, nil
}

// artifactCollector processes glob patterns into files.
type artifactCollector struct {
	*Uploader
	wd string

	mu        sync.Mutex
	seenPaths map[string]bool
	artifacts []*api.Artifact
}

// glob resolves the globs (patterns with * and ** in them).
func (a *Uploader) glob(ctx context.Context, filesCh chan<- string) error {
	// glob is solely responsible for writing to the channel.
	defer close(filesCh)

	if experiments.IsEnabled(ctx, experiments.UseZZGlob) {
		// New zzglob library. Do all globs at once with MultiGlob, which takes
		// care of any necessary parallelism under the hood.
		a.logger.Debug("Searching for %s", a.conf.Paths)
		var patterns []*zzglob.Pattern
		for _, globPath := range strings.Split(a.conf.Paths, ArtifactPathDelimiter) {
			globPath := strings.TrimSpace(globPath)
			if globPath == "" {
				continue
			}
			pattern, err := zzglob.Parse(globPath)
			if err != nil {
				return fmt.Errorf("invalid glob pattern %q: %w", globPath, err)
			}
			patterns = append(patterns, pattern)
		}

		walkDirFunc := func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				a.logger.Warn("Couldn't walk path %s", path)
				return nil
			}
			if d != nil && d.IsDir() {
				a.logger.Warn("One of the glob patterns matched a directory: %s", path)
				return nil
			}
			filesCh <- path
			return nil
		}
		err := zzglob.MultiGlob(ctx, patterns, walkDirFunc, zzglob.TraverseSymlinks(a.conf.GlobResolveFollowSymlinks))
		if err != nil {
			return fmt.Errorf("globbing patterns: %w", err)
		}
		return nil
	}

	// Old go-zglob library. Do each glob one at a time.
	// go-zglob uses fastwalk under the hood to parallelise directory walking.
	globfunc := zglob.Glob
	if a.conf.GlobResolveFollowSymlinks {
		// Follow symbolic links for files & directories while expanding globs
		globfunc = zglob.GlobFollowSymlinks
	}
	for _, globPath := range strings.Split(a.conf.Paths, ArtifactPathDelimiter) {
		globPath := strings.TrimSpace(globPath)
		if globPath == "" {
			continue
		}
		files, err := globfunc(globPath)
		if errors.Is(err, os.ErrNotExist) {
			a.logger.Info("File not found: %s", globPath)
			continue
		}
		if err != nil {
			return fmt.Errorf("resolving glob %s: %w", globPath, err)
		}
		for _, path := range files {
			filesCh <- path
		}
	}
	return nil
}

// worker processes each glob match into an api.Artifact
func (c *artifactCollector) worker(ctx context.Context, filesCh <-chan string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	for {
		var file string
		select {
		case <-ctx.Done():
			return ctx.Err()

		case f, open := <-filesCh:
			if !open {
				return nil
			}
			file = f
		}

		absolutePath, err := filepath.Abs(file)
		if err != nil {
			return fmt.Errorf("resolving absolute path for file %s: %w", file, err)
		}

		// dedupe based on resolved absolutePath
		c.mu.Lock()
		seen := c.seenPaths[absolutePath]
		c.seenPaths[absolutePath] = true
		c.mu.Unlock()

		if seen {
			c.logger.Debug("Skipping duplicate path %s", file)
			continue
		}

		// Ignore directories, we only want files
		if isDir(absolutePath) {
			c.logger.Debug("Skipping directory %s", file)
			continue
		}

		if c.conf.UploadSkipSymlinks && isSymlink(absolutePath) {
			c.logger.Debug("Skipping symlink %s", file)
			continue
		}

		// If a path is absolute, we need to make it relative to the root so that
		// it can be combined with the download destination to make a valid path.
		// This is possibly weird and crazy, this logic dates back to
		// https://github.com/buildkite/agent/commit/8ae46d975aa60d1ae0e2cc0bff7a43d3bf960935
		// from 2014, so I'm replicating it here to avoid breaking things
		basepath := c.wd
		if filepath.IsAbs(file) {
			basepath = "/"
			if runtime.GOOS == "windows" {
				basepath = filepath.VolumeName(absolutePath) + "/"
			}
		}

		path, err := filepath.Rel(basepath, absolutePath)
		if err != nil {
			return fmt.Errorf("resolving relative path for file %s: %w", file, err)
		}

		if experiments.IsEnabled(ctx, experiments.NormalisedUploadPaths) {
			// Convert any Windows paths to Unix/URI form
			path = filepath.ToSlash(path)
		}

		// Build an artifact object using the paths we have.
		artifact, err := c.build(path, absolutePath)
		if err != nil {
			return fmt.Errorf("building artifact: %w", err)
		}

		c.mu.Lock()
		c.artifacts = append(c.artifacts, artifact)
		c.mu.Unlock()
	}
}

func (a *Uploader) build(path string, absolutePath string) (*api.Artifact, error) {
	// Open the file to hash its contents.
	file, err := os.Open(absolutePath)
	if err != nil {
		return nil, fmt.Errorf("opening file %s: %w", absolutePath, err)
	}
	defer file.Close()

	// Generate a SHA-1 and SHA-256 checksums for the file.
	// Writing to hashes never errors, but reading from the file might.
	hash1, hash256 := sha1.New(), sha256.New()
	size, err := io.Copy(io.MultiWriter(hash1, hash256), file)
	if err != nil {
		return nil, fmt.Errorf("reading contents of %s: %w", absolutePath, err)
	}
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
		FileSize:     size,
		Sha1Sum:      sha1sum,
		Sha256Sum:    sha256sum,
		ContentType:  contentType,
	}

	return artifact, nil
}

// createUploader applies some heuristics to the destination to infer which
// uploader to use.
func (a *Uploader) createUploader() (_ workCreator, err error) {
	var dest string
	defer func() {
		if err != nil || dest == "" {
			return
		}
		a.logger.Info("Uploading to %s (%q), using your agent configuration", dest, a.conf.Destination)
	}()

	switch {
	case a.conf.Destination == "":
		a.logger.Info("Uploading to default Buildkite artifact storage")
		return NewBKUploader(a.logger, BKUploaderConfig{
			DebugHTTP: a.conf.DebugHTTP,
		}), nil

	case strings.HasPrefix(a.conf.Destination, "s3://"):
		dest = "Amazon S3"
		return NewS3Uploader(a.logger, S3UploaderConfig{
			Destination: a.conf.Destination,
			DebugHTTP:   a.conf.DebugHTTP,
		})

	case strings.HasPrefix(a.conf.Destination, "gs://"):
		dest = "Google Cloud Storage"
		return NewGSUploader(a.logger, GSUploaderConfig{
			Destination: a.conf.Destination,
			DebugHTTP:   a.conf.DebugHTTP,
		})

	case strings.HasPrefix(a.conf.Destination, "rt://"):
		dest = "Artifactory"
		return NewArtifactoryUploader(a.logger, ArtifactoryUploaderConfig{
			Destination: a.conf.Destination,
			DebugHTTP:   a.conf.DebugHTTP,
		})

	case IsAzureBlobPath(a.conf.Destination):
		dest = "Azure Blob storage"
		return NewAzureBlobUploader(a.logger, AzureBlobUploaderConfig{
			Destination: a.conf.Destination,
		})

	default:
		return nil, fmt.Errorf("invalid upload destination: '%v'. Only s3://*, gs://*, rt://*, or https://*.blob.core.windows.net destinations are allowed. Did you forget to surround your artifact upload pattern in double quotes?", a.conf.Destination)
	}
}

// workCreator implementations convert artifacts into units of work for uploading.
type workCreator interface {
	// The Artifact.URL property is populated with what ever is returned
	// from this method prior to uploading.
	URL(*api.Artifact) string

	// CreateWork provide units of work for uploading an artifact.
	CreateWork(*api.Artifact) ([]workUnit, error)
}

// workUnit implementations upload a whole artifact, or a part of an artifact,
// or could one day do some other work related to an artifact.
type workUnit interface {
	// Artifact returns the artifact being worked on.
	Artifact() *api.Artifact

	// Description describes the unit of work.
	Description() string

	// DoWork does the work.
	DoWork(context.Context) error
}

const (
	artifactStateFinished = "finished"
	artifactStateError    = "error"
)

// Messages passed on channels between goroutines.

// workUnitError is just a tuple (workUnit, error).
type workUnitError struct {
	workUnit workUnit
	err      error
}

// artifactWorkUnits is just a tuple (artifact, int).
type artifactWorkUnits struct {
	artifact  *api.Artifact
	workUnits int
}

// cancelCtx stores a context cancelled when part of an artifact upload has
// failed and needs to fail the whole artifact.
// Go readability notes: "Storing" a context?
// - In a long-lived struct: yeah nah 🙅. Read pkg.go.dev/context
// - Within a single operation ("upload", in this case): nah yeah 👍
type cancelCtx struct {
	ctx    context.Context
	cancel context.CancelCauseFunc
}

func (a *Uploader) upload(ctx context.Context, artifacts []*api.Artifact, uploader workCreator) error {
	perArtifactCtx := make(map[*api.Artifact]cancelCtx)
	for _, artifact := range artifacts {
		actx, cancel := context.WithCancelCause(ctx)
		perArtifactCtx[artifact] = cancelCtx{actx, cancel}
	}

	// workUnitStateCh: multiple worker goroutines --(work unit state)--> state updater
	workUnitStateCh := make(chan workUnitError)
	// artifactWorkUnitsCh: work unit creation --(# work units for artifact)--> state updater
	artifactWorkUnitsCh := make(chan artifactWorkUnits)
	// workUnitsCh: work unit creation --(work unit to be run)--> multiple worker goroutines
	workUnitsCh := make(chan workUnit)
	// stateUpdatesDoneCh: closed when all state updates are complete
	stateUpdatesDoneCh := make(chan struct{})

	// The status updater goroutine: updates batches of artifact states on
	// Buildkite every few seconds.
	var errs []error
	go func() {
		errs = a.statusUpdater(ctx, workUnitStateCh, artifactWorkUnitsCh, stateUpdatesDoneCh)
	}()

	// Worker goroutines that work on work units.
	var workerWG sync.WaitGroup
	for range runtime.GOMAXPROCS(0) {
		workerWG.Add(1)
		go func() {
			defer workerWG.Done()
			a.uploadWorker(ctx, perArtifactCtx, workUnitsCh, workUnitStateCh)
		}()
	}

	// Work creation: creates the work units for each artifact.
	// This must happen after creating goroutines listening on the channels.
	for _, artifact := range artifacts {
		workUnits, err := uploader.CreateWork(artifact)
		if err != nil {
			a.logger.Error("Couldn't create upload workers for artifact %q: %v", artifact.Path, err)
			return err
		}

		// Send the number of work units for this artifact to the state uploader.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case artifactWorkUnitsCh <- artifactWorkUnits{artifact: artifact, workUnits: len(workUnits)}:
		}

		// Send the work units themselves to the workers.
		for _, workUnit := range workUnits {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case workUnitsCh <- workUnit:
			}
		}
	}

	// All work units have been sent to workers, and all counts of pending work
	// units have been sent to the state updater.
	close(workUnitsCh)
	close(artifactWorkUnitsCh)

	a.logger.Debug("Waiting for uploads to complete...")

	// Wait for the workers to finish
	workerWG.Wait()

	// Since the workers are done, all work unit states have been sent to the
	// state updater.
	close(workUnitStateCh)

	a.logger.Debug("Uploads complete, waiting for upload status to be sent to Buildkite...")

	// Wait for the statuses to finish uploading
	<-stateUpdatesDoneCh

	if len(errs) > 0 {
		err := errors.Join(errs...)
		return fmt.Errorf("errors uploading artifacts: %w", err)
	}

	a.logger.Info("Artifact uploads completed successfully")

	return nil
}

func (a *Uploader) uploadWorker(
	ctx context.Context,
	perArtifactCtx map[*api.Artifact]cancelCtx,
	workUnitsCh <-chan workUnit,
	workUnitStateCh chan<- workUnitError,
) {
	for {
		select {
		case <-ctx.Done():
			return

		case workUnit, open := <-workUnitsCh:
			if !open {
				return // Done
			}
			artifact := workUnit.Artifact()
			actx := perArtifactCtx[artifact].ctx
			// Show a nice message that we're starting to upload the file
			a.logger.Info("Uploading %s", workUnit.Description())

			// Upload the artifact and then set the state depending
			// on whether or not it passed. We'll retry the upload
			// a couple of times before giving up.
			err := roko.NewRetrier(
				roko.WithMaxAttempts(10),
				roko.WithStrategy(roko.Constant(5*time.Second)),
			).DoWithContext(actx, func(r *roko.Retrier) error {
				if err := workUnit.DoWork(actx); err != nil {
					a.logger.Warn("%s (%s)", err, r)
					return err
				}
				return nil
			})

			// If it failed, abort any other work items for this artifact.
			if err != nil {
				a.logger.Info("Upload failed for %s", workUnit.Description())
				perArtifactCtx[workUnit.Artifact()].cancel(err)
			}

			// Let the state updater know how the work went.
			select {
			case <-ctx.Done(): // the main context, not the artifact ctx
				return // ctx.Err()

			case workUnitStateCh <- workUnitError{workUnit: workUnit, err: err}:
			}
		}
	}
}

func (a *Uploader) statusUpdater(
	ctx context.Context,
	workUnitStateCh <-chan workUnitError,
	artifactWorkUnitsCh <-chan artifactWorkUnits,
	doneCh chan<- struct{},
) []error {
	defer close(doneCh)

	// Errors that caused an artifact upload to fail, or a batch fail to update.
	var errs []error

	// artifact -> number of work units that are incomplete
	pendingWorkUnits := make(map[*api.Artifact]int)

	// States that haven't been updated on Buildkite yet.
	statesToUpload := make(map[string]string) // artifact ID -> state

	// When this ticks, upload any pending artifact states.
	updateTicker := time.NewTicker(1 * time.Second)

	for {
		select {
		case <-ctx.Done():
			return errs

		case <-updateTicker.C:
			if len(statesToUpload) == 0 { // no news from the frontier
				break
			}
			// Post an update
			timeout := 5 * time.Second

			// Update the states of the artifacts in bulk.
			err := roko.NewRetrier(
				roko.WithMaxAttempts(10),
				roko.WithStrategy(roko.ExponentialSubsecond(500*time.Millisecond)),
			).DoWithContext(ctx, func(r *roko.Retrier) error {
				ctxTimeout := ctx
				if timeout != 0 {
					var cancel func()
					ctxTimeout, cancel = context.WithTimeout(ctx, timeout)
					defer cancel()
				}

				_, err := a.apiClient.UpdateArtifacts(ctxTimeout, a.conf.JobID, statesToUpload)
				if err != nil {
					a.logger.Warn("%s (%s)", err, r)
				}

				// after four attempts (0, 1, 2, 3)...
				if r.AttemptCount() == 3 {
					// The short timeout has given us fast feedback on the first couple of attempts,
					// but perhaps the server needs more time to complete the request, so fall back to
					// the default HTTP client timeout.
					a.logger.Debug("UpdateArtifacts timeout (%s) removed for subsequent attempts", timeout)
					timeout = 0
				}

				return err
			})
			if err != nil {
				a.logger.Error("Error updating artifact states: %s", err)
				errs = append(errs, err)
			}

			a.logger.Debug("Updated %d artifact states", len(statesToUpload))
			clear(statesToUpload)

		case awu, open := <-artifactWorkUnitsCh:
			if !open {
				// Set it to nil so this select branch never happens again.
				artifactWorkUnitsCh = nil
				// If both input channels are nil, we're done!
				if workUnitStateCh == nil {
					return errs
				}
			}

			// Track how many pending work units there should be per artifact.
			// Use += in case some work units for this artifact already completed.
			pendingWorkUnits[awu.artifact] += awu.workUnits
			if pendingWorkUnits[awu.artifact] != 0 {
				break
			}
			// The whole artifact is complete, add it to the next batch of
			// states to upload.
			statesToUpload[awu.artifact.ID] = artifactStateFinished
			a.logger.Debug("Artifact `%s` has entered state `%s`", awu.artifact.ID, artifactStateFinished)

		case workUnitState, open := <-workUnitStateCh:
			if !open {
				// Set it to nil so this select branch never happens again.
				workUnitStateCh = nil
				// If both input channels are nil, we're done!
				if artifactWorkUnitsCh == nil {
					return errs
				}
			}
			artifact := workUnitState.workUnit.Artifact()
			if workUnitState.err != nil {
				// The work unit failed, so the whole artifact upload has failed.
				errs = append(errs, workUnitState.err)
				statesToUpload[artifact.ID] = artifactStateError
				a.logger.Debug("Artifact `%s` has entered state `%s`", artifact.ID, artifactStateError)
				break
			}
			// The work unit is complete - it's no longer pending.
			pendingWorkUnits[artifact]--
			if pendingWorkUnits[artifact] != 0 {
				break
			}
			// No pending units remain, so the whole artifact is complete.
			// Add it to the next batch of states to upload.
			statesToUpload[artifact.ID] = artifactStateFinished
			a.logger.Debug("Artifact `%s` has entered state `%s`", artifact.ID, artifactStateFinished)
		}
	}
}

func singleUnitDescription(artifact *api.Artifact) string {
	return fmt.Sprintf("%s %s (%s)", artifact.ID, artifact.Path, humanize.IBytes(uint64(artifact.FileSize)))
}
