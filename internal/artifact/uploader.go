package artifact

import (
	"cmp"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"iter"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"drjosh.dev/zzglob"
	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/internal/mime"
	"github.com/buildkite/roko"
	"github.com/dustin/go-humanize"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const ArtifactFallbackMimeType = "binary/octet-stream"

type UploaderConfig struct {
	// The ID of the Job
	JobID string

	// The path of the uploads
	Paths string

	// Where we'll be uploading artifacts
	Destination string

	// A specific Content-Type to use for all artifacts
	ContentType string

	// Standard HTTP options.
	DebugHTTP    bool
	TraceHTTP    bool
	DisableHTTP2 bool

	// When true, disables parsing Paths as globs; treat each path literally.
	Literal bool

	// The delimiter used to split Paths into multiple paths/globs.
	Delimiter string

	// Whether to follow symbolic links when resolving globs
	GlobResolveFollowSymlinks bool

	// Whether to not upload symlinks
	UploadSkipSymlinks bool

	// Whether to allow multipart uploads to the BK-hosted bucket
	AllowMultipart bool

	// How many upload operations to run concurrently. When zero, defaults to
	// DefaultUploadConcurrency.
	UploadConcurrency int
}

type Uploader struct {
	// The upload config
	conf UploaderConfig

	// The logger instance to use
	logger *slog.Logger

	// The APIClient that will be used when uploading jobs
	apiClient APIClient
}

type artifactUploadTimings struct {
	collectDuration time.Duration
	createDuration  time.Duration
	uploadDuration  time.Duration
	stateDuration   time.Duration

	artifactCount    int
	artifactBytes    int64
	batches          int
	workUnits        int
	maxWorkerCount   int
	stateUpdateCount int
}

type artifactUploadBatchStats struct {
	workUnits      int
	workerCount    int
	uploadDuration time.Duration

	stateUpdateDuration time.Duration
	stateUpdateCount    int
}

func (t *artifactUploadTimings) addArtifacts(artifacts []*api.Artifact) {
	t.artifactCount = len(artifacts)
	for _, artifact := range artifacts {
		t.artifactBytes += artifact.FileSize
	}
}

func (t *artifactUploadTimings) addCreateBatch(duration time.Duration) {
	t.createDuration += duration
	t.batches++
}

func (t *artifactUploadTimings) addUploadBatch(stats artifactUploadBatchStats) {
	t.uploadDuration += stats.uploadDuration
	t.stateDuration += stats.stateUpdateDuration
	t.workUnits += stats.workUnits
	t.stateUpdateCount += stats.stateUpdateCount
	t.maxWorkerCount = max(t.maxWorkerCount, stats.workerCount)
}

func (t *artifactUploadTimings) setSpanAttributes(ctx context.Context) {
	trace.SpanFromContext(ctx).SetAttributes(
		attribute.Int("artifact.count", t.artifactCount),
		attribute.Int64("artifact.bytes", t.artifactBytes),
		attribute.Int("artifact.batch_count", t.batches),
		attribute.Int("artifact.work_unit_count", t.workUnits),
		attribute.Int("artifact.max_workers", t.maxWorkerCount),
		attribute.Int("artifact.state_update_count", t.stateUpdateCount),
		attribute.Int64("artifact.collect_ms", t.collectDuration.Milliseconds()),
		attribute.Int64("artifact.create_ms", t.createDuration.Milliseconds()),
		attribute.Int64("artifact.upload_ms", t.uploadDuration.Milliseconds()),
		attribute.Int64("artifact.state_update_ms", t.stateDuration.Milliseconds()),
	)
}

func (t *artifactUploadTimings) logSummary(ctx context.Context, l *slog.Logger) {
	l.InfoContext(ctx, "Artifact upload timings",
		"collect_duration", t.collectDuration,
		"create_duration", t.createDuration,
		"upload_duration", t.uploadDuration,
		"state_update_duration", t.stateDuration,
		"artifact_count", t.artifactCount,
		"artifact_bytes", t.artifactBytes,
		"batch_count", t.batches,
		"work_unit_count", t.workUnits,
		"max_workers", t.maxWorkerCount,
		"state_update_count", t.stateUpdateCount,
	)
}

func NewUploader(l *slog.Logger, ac APIClient, c UploaderConfig) *Uploader {
	return &Uploader{
		logger:    l,
		apiClient: ac,
		conf:      c,
	}
}

func DefaultUploadConcurrency() int {
	return runtime.GOMAXPROCS(0)
}

func (a *Uploader) Upload(ctx context.Context) error {
	// Create artifact structs for all the files we need to upload
	timings := &artifactUploadTimings{}
	collectStart := time.Now()
	artifacts, err := a.collect(ctx)
	timings.collectDuration = time.Since(collectStart)
	if err != nil {
		return fmt.Errorf("collecting artifacts: %w", err)
	}

	if len(artifacts) == 0 {
		a.logger.InfoContext(ctx, "No files matched artifact paths", "path", a.conf.Paths)
		return nil
	}

	a.logger.InfoContext(ctx, "Found files matching artifact paths", "file_count", len(artifacts), "paths", a.conf.Paths)
	timings.addArtifacts(artifacts)

	// Determine what uploader to use
	uploader, err := a.createUploader(ctx)
	if err != nil {
		return fmt.Errorf("creating uploader: %w", err)
	}

	// Set the URLs of the artifacts based on the uploader
	for _, artifact := range artifacts {
		artifact.URL = uploader.URL(artifact)
	}

	// Batch-create artifact records on Buildkite and upload each batch
	// immediately. This keeps presigned upload URLs fresh — if all
	// artifacts were were created upfront in a single batch, URLs for artifacts near the
	// end of the queue could expire before the agent gets to them.
	batchCreator := NewArtifactBatchCreator(a.logger, a.apiClient, BatchCreatorConfig{
		JobID:                  a.conf.JobID,
		Artifacts:              artifacts,
		UploadDestination:      a.conf.Destination,
		CreateArtifactsTimeout: 10 * time.Second,
		AllowMultipart:         a.conf.AllowMultipart,
		OnBatchCreated: func(count int, duration time.Duration) {
			timings.addCreateBatch(duration)
			a.logger.DebugContext(ctx, "Created artifacts", "artifact_count", count, "duration", duration)
		},
	})
	for chunk, err := range batchCreator.Batches(ctx) {
		if err != nil {
			return err
		}
		stats, err := a.upload(ctx, chunk, uploader)
		timings.addUploadBatch(stats)
		if err != nil {
			return fmt.Errorf("uploading artifacts: %w", err)
		}
		a.logger.DebugContext(ctx, "Uploaded artifact work units", "work_unit_count", stats.workUnits, "worker_count", stats.workerCount, "duration", stats.uploadDuration)
	}

	timings.setSpanAttributes(ctx)
	timings.logSummary(ctx, a.logger)

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
	defer cancel(nil)
	var wg sync.WaitGroup
	for range runtime.GOMAXPROCS(0) {
		wg.Go(func() {
			if err := ac.worker(wctx, filesCh); err != nil {
				cancel(err)
			}
		})
	}

	fileFinder := a.glob
	if a.conf.Literal {
		fileFinder = a.literal
	}

	// Start resolving globs (or not) and sending file paths to workers.
	a.logger.DebugContext(ctx, "Searching for artifact paths", "path", a.conf.Paths)
	searchStart := time.Now()
	if err := fileFinder(wctx, a.paths(), filesCh); err != nil {
		cancel(err)
	}
	a.logger.DebugContext(ctx, "Artifact path discovery finished", "duration", time.Since(searchStart))

	drainStart := time.Now()
	wg.Wait()
	a.logger.DebugContext(ctx, "Artifact collection workers drained", "duration", time.Since(drainStart))

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

func (a *Uploader) paths() iter.Seq[string] {
	if a.conf.Delimiter == "" {
		// Don't do any splitting.
		return slices.Values([]string{a.conf.Paths})
	}
	return strings.SplitSeq(a.conf.Paths, a.conf.Delimiter)
}

func (a *Uploader) literal(ctx context.Context, paths iter.Seq[string], filesCh chan<- string) error {
	// literal is solely responsible for writing to the channel
	defer close(filesCh)

	for path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case filesCh <- path:
		}
	}
	return nil
}

// glob resolves the globs (patterns with * and ** in them).
func (a *Uploader) glob(ctx context.Context, paths iter.Seq[string], filesCh chan<- string) error {
	// glob is solely responsible for writing to the channel.
	defer close(filesCh)

	// Do all globs at once with MultiGlob, which takes care of any necessary
	// parallelism under the hood.
	var patterns []*zzglob.Pattern
	for globPath := range paths {
		globPath = strings.TrimSpace(globPath)
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
			a.logger.WarnContext(ctx, "Could not walk artifact path", "path", path, "error", err)
			return nil
		}
		if d != nil && d.IsDir() {
			a.logger.WarnContext(ctx, "Artifact glob pattern matched a directory", "path", path)
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case filesCh <- path:
			return nil
		}
	}
	err := zzglob.MultiGlob(ctx, patterns, walkDirFunc, zzglob.TraverseSymlinks(a.conf.GlobResolveFollowSymlinks))
	if err != nil {
		return fmt.Errorf("globbing patterns: %w", err)
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
			c.logger.DebugContext(ctx, "Skipping duplicate artifact path", "path", file)
			continue
		}

		// Ignore directories, we only want files
		if isDir(absolutePath) {
			c.logger.DebugContext(ctx, "Skipping artifact directory", "path", file)
			continue
		}

		if c.conf.UploadSkipSymlinks && isSymlink(absolutePath) {
			c.logger.DebugContext(ctx, "Skipping artifact symlink", "path", file)
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

		// Convert any Windows paths to Unix/URI form
		path = filepath.ToSlash(path)

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

func (a *Uploader) build(path, absolutePath string) (*api.Artifact, error) {
	// Open the file to hash its contents.
	file, err := os.Open(absolutePath)
	if err != nil {
		return nil, fmt.Errorf("opening file %s: %w", absolutePath, err)
	}
	defer file.Close() //nolint:errcheck // File is only open for read.

	// Generate a SHA-1 and SHA-256 checksums for the file.
	hash1, hash256 := sha1.New(), sha256.New()
	size, err := io.Copy(io.MultiWriter(hash1, hash256), file)
	if err != nil {
		return nil, fmt.Errorf("hashing artifact file %s: %w", absolutePath, err)
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
func (a *Uploader) createUploader(ctx context.Context) (_ workCreator, err error) {
	var dest string
	defer func() {
		if err != nil || dest == "" {
			return
		}
		a.logger.InfoContext(ctx, "Using configured artifact upload destination", "destination", dest, "url", a.conf.Destination)
	}()

	switch {
	case a.conf.Destination == "":
		a.logger.InfoContext(ctx, "Uploading to default Buildkite artifact storage")
		return NewBKUploader(a.logger, BKUploaderConfig{
			DebugHTTP:    a.conf.DebugHTTP,
			TraceHTTP:    a.conf.TraceHTTP,
			DisableHTTP2: a.conf.DisableHTTP2,
		}), nil

	case strings.HasPrefix(a.conf.Destination, "s3://"):
		dest = "Amazon S3"
		return NewS3Uploader(ctx, a.logger, S3UploaderConfig{
			Destination: a.conf.Destination,
		})

	case strings.HasPrefix(a.conf.Destination, "gs://"):
		dest = "Google Cloud Storage"
		return NewGSUploader(ctx, a.logger, GSUploaderConfig{
			Destination: a.conf.Destination,
		})

	case strings.HasPrefix(a.conf.Destination, "rt://"):
		dest = "Artifactory"
		return NewArtifactoryUploader(a.logger, ArtifactoryUploaderConfig{
			Destination:  a.conf.Destination,
			DebugHTTP:    a.conf.DebugHTTP,
			TraceHTTP:    a.conf.TraceHTTP,
			DisableHTTP2: a.conf.DisableHTTP2,
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
	DoWork(context.Context) (*api.ArtifactPartETag, error)
}

// workUnitResult is just a tuple (workUnit, partETag | error).
type workUnitResult struct {
	workUnit workUnit
	partETag *api.ArtifactPartETag
	err      error
}

// artifactUploadWorker contains the shared state between the worker goroutines
// and the state uploader.
type artifactUploadWorker struct {
	*Uploader

	// A tracker for every artifact.
	// The map is written at the start of upload, and other goroutines only read
	// afterwards.
	trackers map[*api.Artifact]*artifactTracker

	stateUpdateDuration time.Duration
	stateUpdateCount    int
}

// artifactTracker tracks the amount of work pending for an artifact.
type artifactTracker struct {
	// All the work for uploading this artifact.
	work []workUnit

	// Normally storing a context in a struct is a bad idea. It's explicitly
	// called out in pkg.go.dev/context as a no-no (unless you are required to
	// store a context for some reason like interface implementation or
	// backwards compatibility).
	//
	// The main reason is that contexts are intended to be associated with a
	// clear chain of function calls (i.e. work), and having contexts stored in
	// a struct somewhere means there's confusion over which context should be
	// used for a given function call. (Do you use one passed in, or from a
	// struct? Do you listen for Done on both? Which context values apply?)
	//
	// So here's how this context is situated within the chain of work, and
	// how it will be used:
	//
	// This context applies to all the work units for this artifact.
	// It is a child context of the context passed to Uploader.upload, which
	// should be exactly the same context that Uploader.upload passes to
	// artifactUploadWorker.
	// Canceling this context cancels all work associated with this artifact
	// (across the fixed-size pool of worker goroutines).
	ctx    context.Context
	cancel context.CancelCauseFunc

	// pendingWork is the number of incomplete units for this artifact.
	// This is set once at the start of upload, and then decremented by the
	// state updater goroutine as work units complete.
	pendingWork int

	// State that will be uploaded to BK when the artifact is finished or errored.
	// Only the state updater goroutine writes this.
	api.ArtifactState
}

func (a *Uploader) upload(ctx context.Context, artifacts []*api.Artifact, uploader workCreator) (artifactUploadBatchStats, error) {
	worker := &artifactUploadWorker{
		Uploader: a,
		trackers: make(map[*api.Artifact]*artifactTracker),
	}
	stats := artifactUploadBatchStats{}

	// Create work and trackers for each artifact.
	var workUnitCount int
	for _, artifact := range artifacts {
		workUnits, err := uploader.CreateWork(artifact)
		if err != nil {
			a.logger.ErrorContext(ctx, "Could not create artifact upload workers", "artifact", artifact.Path, "error", err)
			return stats, err
		}
		workUnitCount += len(workUnits)

		actx, acancel := context.WithCancelCause(ctx)
		worker.trackers[artifact] = &artifactTracker{
			ctx:         actx,
			cancel:      acancel,
			work:        workUnits,
			pendingWork: len(workUnits),
			ArtifactState: api.ArtifactState{
				ID:        artifact.ID,
				Multipart: len(workUnits) > 1,
			},
		}
	}

	workerCount := a.uploadWorkerCount(workUnitCount)
	stats.workUnits = workUnitCount
	stats.workerCount = workerCount
	if workerCount == 0 {
		return stats, nil
	}
	a.logger.DebugContext(ctx, "Uploading artifact work units", "work_unit_count", workUnitCount, "worker_count", workerCount)

	// unitsCh: work unit creation --(work unit to be run)--> multiple worker goroutines
	unitsCh := make(chan workUnit)
	// resultsCh: multiple worker goroutines --(work unit result)--> state updater
	resultsCh := make(chan workUnitResult)
	// errCh: receives the final error from the status updater
	errCh := make(chan error, 1)

	// The status updater goroutine: updates batches of artifact states on
	// Buildkite every few seconds.
	go worker.stateUpdater(ctx, resultsCh, errCh)

	// Worker goroutines that work on work units.
	var wg sync.WaitGroup
	for range workerCount {
		wg.Go(func() { worker.doWorkUnits(ctx, unitsCh, resultsCh) })
	}

	// Send the work units for each artifact to the workers.
	// This must happen after creating worker goroutines listening on workUnitsCh.
	uploadStart := time.Now()
	for _, tracker := range worker.trackers {
		for _, workUnit := range tracker.work {
			select {
			case <-ctx.Done():
				return stats, ctx.Err()
			case unitsCh <- workUnit:
			}
		}
	}

	// All work units have been sent to workers.
	close(unitsCh)

	a.logger.DebugContext(ctx, "Waiting for artifact uploads to complete")

	// Wait for the workers to finish
	wg.Wait()
	stats.uploadDuration = time.Since(uploadStart)

	// Since the workers are done, all work unit states have been sent to the
	// state updater.
	close(resultsCh)

	a.logger.DebugContext(ctx, "Artifact uploads complete; waiting for status update")

	// Wait for the statuses to finish uploading
	if err := <-errCh; err != nil {
		stats.stateUpdateDuration = worker.stateUpdateDuration
		stats.stateUpdateCount = worker.stateUpdateCount
		return stats, fmt.Errorf("errors uploading artifacts: %w", err)
	}
	stats.stateUpdateDuration = worker.stateUpdateDuration
	stats.stateUpdateCount = worker.stateUpdateCount

	a.logger.InfoContext(ctx, "Artifact uploads completed successfully")

	return stats, nil
}

func (a *Uploader) uploadWorkerCount(workUnitCount int) int {
	if workUnitCount <= 0 {
		return 0
	}

	concurrency := a.conf.UploadConcurrency
	if concurrency <= 0 {
		concurrency = DefaultUploadConcurrency()
	}

	return min(concurrency, workUnitCount)
}

func (a *artifactUploadWorker) doWorkUnits(ctx context.Context, unitsCh <-chan workUnit, resultsCh chan<- workUnitResult) {
	for {
		select {
		case <-ctx.Done():
			return

		case workUnit, open := <-unitsCh:
			if !open {
				return // Done
			}
			tracker := a.trackers[workUnit.Artifact()]
			// Show a nice message that we're starting to upload the file
			a.logger.InfoContext(tracker.ctx, "Uploading artifact", "artifact", workUnit.Description())

			// Upload the artifact and then set the state depending
			// on whether or not it passed. We'll retry the upload
			// a couple of times before giving up.
			r := roko.NewRetrier(
				roko.WithMaxAttempts(10),
				roko.WithStrategy(roko.Constant(5*time.Second)),
			)
			partETag, err := roko.DoFunc(tracker.ctx, r, func(r *roko.Retrier) (*api.ArtifactPartETag, error) {
				etag, err := workUnit.DoWork(tracker.ctx)
				if err != nil {
					a.logger.WarnContext(tracker.ctx, "Artifact upload failed; retrying", "error", err, "retry", r)
				}
				return etag, err
			})
			// If it failed, abort any other work items for this artifact.
			if err != nil {
				a.logger.InfoContext(tracker.ctx, "Artifact upload failed", "artifact", workUnit.Description(), "error", err)
				tracker.cancel(err)
				// then the error is sent to the status updater
			}

			// Let the state updater know how the work went.
			select {
			case <-ctx.Done(): // Note: the main context, not the artifact tracker context
				return

			case resultsCh <- workUnitResult{workUnit: workUnit, partETag: partETag, err: err}:
			}
		}
	}
}

func (a *artifactUploadWorker) stateUpdater(ctx context.Context, resultsCh <-chan workUnitResult, stateUpdaterErrCh chan<- error) {
	var errs []error

	// When this ticks, upload any pending artifact states as a batch.
	updateTicker := time.NewTicker(1 * time.Second)

selectLoop:
	for {
		select {
		case <-ctx.Done():
			break selectLoop

		case <-updateTicker.C:
			// Note: updateStates removes trackers for completed states.
			if err := a.updateStates(ctx); err != nil {
				errs = append(errs, err)
			}

		case result, open := <-resultsCh:
			if !open {
				// No more input: we're done!
				break selectLoop
			}
			artifact := result.workUnit.Artifact()
			tracker := a.trackers[artifact]

			if result.err != nil {
				// The work unit failed, so the whole artifact upload has failed.
				errs = append(errs, result.err)
				tracker.State = "error"
				a.logger.DebugContext(ctx, "Artifact state changed", "artifact_id", tracker.ID, "state", tracker.State)
				continue
			}

			// The work unit is complete - it's no longer pending.
			if partETag := result.partETag; partETag != nil {
				tracker.MultipartETags = append(tracker.MultipartETags, *partETag)
			}

			tracker.pendingWork--
			if tracker.pendingWork > 0 {
				continue
			}

			// No pending units remain, so the whole artifact is complete.
			// Add it to the next batch of states to upload.
			tracker.State = "finished"
			a.logger.DebugContext(ctx, "Artifact state changed", "artifact_id", tracker.ID, "state", tracker.State)
		}
	}

	// Upload any remaining states.
	if err := a.updateStates(ctx); err != nil {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		stateUpdaterErrCh <- errors.Join(errs...)
	}
	close(stateUpdaterErrCh)
}

// updateStates uploads terminal artifact states to Buildkite in a batch.
func (a *artifactUploadWorker) updateStates(ctx context.Context) error {
	// Only upload states that are finished or error.
	var statesToUpload []api.ArtifactState
	var trackersToMarkSent []*artifactTracker
	for _, tracker := range a.trackers {
		switch tracker.State {
		case "finished", "error":
			// Only send these states.
		default:
			continue
		}
		// This artifact is complete, move it from a.trackers to statesToUpload.
		statesToUpload = append(statesToUpload, tracker.ArtifactState)
		trackersToMarkSent = append(trackersToMarkSent, tracker)
	}

	if len(statesToUpload) == 0 { // no news from the frontier
		return nil
	}

	for _, state := range statesToUpload {
		// Ensure ETags are in ascending order by part number.
		// This is required by S3.
		slices.SortFunc(state.MultipartETags, func(a, b api.ArtifactPartETag) int {
			return cmp.Compare(a.PartNumber, b.PartNumber)
		})
	}

	// Post the update
	timeout := 5 * time.Second

	// Update the states of the artifacts in bulk.
	started := time.Now()
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
			a.logger.WarnContext(ctx, "Artifact state update failed; retrying", "error", err, "retry", r)
		}

		// after four attempts (0, 1, 2, 3)...
		if r.AttemptCount() == 3 {
			// The short timeout has given us fast feedback on the first couple of attempts,
			// but perhaps the server needs more time to complete the request, so fall back to
			// the default HTTP client timeout.
			a.logger.DebugContext(ctx, "Artifact state update timeout removed for subsequent attempts", "duration", timeout)
			timeout = 0
		}

		return err
	})
	duration := time.Since(started)
	a.stateUpdateDuration += duration
	a.stateUpdateCount++
	if err != nil {
		a.logger.ErrorContext(ctx, "Error updating artifact states", "error", err)
		return err
	}

	for _, tracker := range trackersToMarkSent {
		// Don't send this state again.
		tracker.State = "sent"
	}
	a.logger.DebugContext(ctx, "Updated artifact states", "artifact_count", len(statesToUpload), "duration", duration)
	return nil
}

// singleUnitDescription can be used by uploader implementations to describe
// artifact uploads consisting of a single work unit.
func singleUnitDescription(artifact *api.Artifact) string {
	return fmt.Sprintf("%s %s (%s)", artifact.ID, artifact.Path, humanize.IBytes(uint64(artifact.FileSize)))
}
