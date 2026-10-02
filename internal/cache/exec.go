package cache

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/internal/cache/archive"
	"github.com/buildkite/agent/v4/internal/cache/configuration"
	"github.com/buildkite/agent/v4/internal/cache/store"
	"github.com/buildkite/agent/v4/logger"
	"go.opentelemetry.io/otel"
)

// outputTargetPath is a target path added to target_paths to give the output sidecar its own address that plain cache restore never matches; "<...>" can't be a real file path and stands out in the registry's entry list.
const outputTargetPath = "<cache-exec-output>"

// Command runs the wrapped command with the given streams; a non-nil error means it failed and is returned by RunExec.
type Command func(stdout, stderr io.Writer) error

// RunExec runs command unless the one cache in cfg.Names holds an exact-key result, in which case it restores target_paths and replays the recorded output; on success after a miss it saves both, and cache failures follow cfg.FailOnError.
func RunExec(ctx context.Context, l logger.Logger, apiClient *api.Client, cfg Config, stdout, stderr io.Writer, command Command) error {
	if len(cfg.Names) != 1 {
		return fmt.Errorf("cache exec needs exactly one --name, got %d", len(cfg.Names))
	}
	c, _, err := newClient(l, apiClient, cfg)
	if err == nil && c == nil {
		err = fmt.Errorf("cache names not found in configuration: %s", cfg.Names[0])
	}
	if err != nil {
		return RunUncached(l, cfg.FailOnError, err, stdout, stderr, command)
	}
	c.onProgress = func(cacheID, stage, message string, _, _ int) {
		l.WithFields(
			logger.StringField("cache_id", cacheID),
			logger.StringField("stage", stage),
			logger.StringField("message", message),
		).Debugf("Cache progress")
	}
	redactions := cfg.Redactions
	if redactions == nil {
		redactions = func(context.Context) ([]string, error) { return nil, nil }
	}
	return c.exec(ctx, l, cfg.Names[0], cfg.FailOnError, redactions, stdout, stderr, command)
}

// RunUncached runs command without the cache because of err, a cache problem, or returns err if failOnError.
func RunUncached(l logger.Logger, failOnError bool, err error, stdout, stderr io.Writer, command Command) error {
	if failOnError {
		return err
	}
	l.Warnf("%v; running the command without caching", err)
	return command(stdout, stderr)
}

func (c *client) exec(ctx context.Context, l logger.Logger, cacheID string, failOnError bool, redactions func(context.Context) ([]string, error), stdout, stderr io.Writer, command Command) error {
	ctx, span := otel.Tracer("github.com/buildkite/agent/v4/internal/cache").Start(ctx, "Client.exec")
	defer span.End()

	cacheConfig, err := c.findCache(cacheID)
	if err != nil {
		return err
	}
	if slices.ContainsFunc(cacheConfig.CacheKey, func(p configuration.KeyPart) bool { return p.FallbackLimit }) {
		l.Infof("cache exec ignores fallback_limit for %q: only an exact cache_key match skips the command", cacheID)
	}

	s := newStreams(stdout, stderr)

	// A cache problem before anything was restored is a miss: run uncached unless failOnError.
	runUncached := func(err error) error {
		return RunUncached(l, failOnError, err, s.Stdout(), s.Stderr(), command)
	}

	// Without the secrets, a new result can't be saved safely, but a saved result was redacted when it was saved, so it can still be replayed.
	needles, err := redactions(ctx)
	canSave := err == nil
	if err != nil {
		err = fmt.Errorf("couldn't get the secrets to redact from saved output: %w", err)
		if failOnError {
			return err
		}
		l.Warnf("%v; a cached result can still be used, but a new one won't be saved", err)
	}

	// Resolve the key before the command can change its inputs, with every part mandatory: replaying a fallback's output would misrepresent what ran.
	cacheKey, err := c.resolveCacheKey(cacheConfig)
	if err != nil {
		return runUncached(fmt.Errorf("failed to resolve cache key for %q: %w", cacheID, err))
	}
	for i := range cacheKey {
		cacheKey[i].Mandatory = true
	}

	s.header("--- :package: Restoring cache...")
	hit, err := c.execRestore(ctx, l, cacheConfig, cacheKey, failOnError, s)
	if err != nil || hit {
		return err
	}

	if !canSave {
		s.header("+++ :package: No cached result, running command (not saving the result)")
		return command(s.Stdout(), s.Stderr())
	}

	rec, err := newOutputRecorder(needles)
	if err != nil {
		return runUncached(err)
	}
	defer func() { _ = os.Remove(rec.Path()) }()

	s.header("+++ :package: No cached result, running command")
	runErr := command(io.MultiWriter(s.Stdout(), rec.Stdout()), io.MultiWriter(s.Stderr(), rec.Stderr()))
	output, recordErr := rec.Close()
	if runErr != nil {
		return runErr
	}
	s.header("--- :package: Saving cache...")

	// The command may have registered secrets (e.g. with secret get) after recording started, so redact the recording again with the final set before saving anything.
	finalNeedles, err := redactions(ctx)
	if err != nil {
		err = fmt.Errorf("couldn't get the secrets to redact from saved output, so not saving cache %q: %w", cacheID, err)
		if failOnError {
			return err
		}
		l.Warnf("%v", err)
		return nil
	}
	if recordErr == nil && slices.ContainsFunc(finalNeedles, func(n string) bool { return !slices.Contains(needles, n) }) {
		output, recordErr = reRedactOutput(output.ArchivePath, finalNeedles)
		if output != nil {
			defer func() { _ = os.Remove(output.ArchivePath) }()
		}
	}

	return c.execSave(ctx, l, cacheConfig, cacheKey, failOnError, output, recordErr)
}

// execRestore reports a hit, restoring target_paths and replaying output, only for an exact main entry with recorded output for its archive; otherwise the command runs.
func (c *client) execRestore(ctx context.Context, l logger.Logger, cacheConfig *configuration.Cache, cacheKey []api.CacheKeyPart, failOnError bool, s *streams) (bool, error) {
	cacheID := cacheConfig.Name

	// Check for matching output before restore downloads or touches target_paths, so files without it (e.g. from plain save) are left alone.
	var (
		outputFile    string
		cleanup       func()
		confirmOutput func()
		outputErr     error
		noOutput      bool
	)
	result, err := c.restore(ctx, cacheID, cacheKey, func(main api.CacheEntryRetrieveResp) bool {
		if len(main.Blobs) == 0 {
			return false
		}
		outputFile, cleanup, confirmOutput, outputErr = c.restoreOutput(ctx, cacheConfig, outputCacheKey(cacheKey, main.Blobs[0].Digest.Value))
		noOutput = outputFile == "" && outputErr == nil
		return outputFile != ""
	})
	if cleanup != nil {
		defer cleanup()
	}
	if outputErr != nil {
		if failOnError {
			return false, fmt.Errorf("failed to restore cached output for %q: %w", cacheID, outputErr)
		}
		l.Warnf("Failed to restore cached output for %q: %v; running the command", cacheID, outputErr)
		return false, nil
	}
	if err != nil {
		// As in restore, fail rather than run the command against half-restored target paths.
		if failOnError || errors.Is(err, errRestoreMutatedTargets) {
			l.Warnf("%s", restoreReport(cacheID, result, err))
			return false, fmt.Errorf("failed to restore cache %q: %w", cacheID, err)
		}
		l.Warnf("%s; running the command", restoreReport(cacheID, result, err))
		return false, nil
	}
	if noOutput {
		result.NotRestoredReason = "Cache not restored: no recorded command output for these files"
	}
	l.Infof("%s", restoreReport(cacheID, result, nil))
	if !result.CacheRestored || !result.CacheHit {
		return false, nil
	}

	confirmOutput()
	s.header("+++ :package: Replaying output from cache (command was not run)")
	if err := replayOutput(outputFile, s.Stdout(), s.Stderr()); err != nil {
		return true, fmt.Errorf("failed to replay cached output for %q: %w", cacheID, err)
	}
	return true, nil
}

// restoreOutput downloads and validates the output sidecar (empty path = miss, else the caller runs cleanup, and confirm once the main entry is restored); missing or unreadable blobs are invalidated and treated as a miss.
func (c *client) restoreOutput(ctx context.Context, cacheConfig *configuration.Cache, outputKey []api.CacheKeyPart) (path string, cleanup, confirm func(), err error) {
	startTime := time.Now()

	retrieveResp, exists, err := c.retrieveEntry(ctx, outputTargetPaths(cacheConfig.TargetPaths), outputKey)
	if err != nil {
		return "", nil, nil, fmt.Errorf("failed to retrieve cache: %w", err)
	}
	if !exists {
		return "", nil, nil, nil
	}
	if err := validateCacheStore(retrieveResp.Store, c.bucketURL); err != nil {
		return "", nil, nil, fmt.Errorf("invalid cache store configuration: %w", err)
	}

	tmpDir, file, transferInfo, err := c.downloadCache(ctx, retrieveResp, c.bucketURL)
	if err != nil {
		if errors.Is(err, store.ErrBlobNotFound) || errors.Is(err, ErrDigestMismatch) {
			slog.Warn("cached command output is missing or corrupt, treating as miss and invalidating entry",
				"cache_id", cacheConfig.Name, "err", err)
			c.invalidateStaleEntry(ctx, retrieveResp)
			return "", nil, nil, nil
		}
		return "", nil, nil, fmt.Errorf("failed to download cache: %w", err)
	}
	cleanup = func() { _ = os.RemoveAll(tmpDir) }

	if err := replayOutput(file, io.Discard, io.Discard); err != nil {
		cleanup()
		slog.Warn("cached command output is unreadable, treating as miss and invalidating entry",
			"cache_id", cacheConfig.Name, "err", err)
		c.invalidateStaleEntry(ctx, retrieveResp)
		return "", nil, nil, nil
	}

	// Refresh the sidecar's retention so it doesn't expire while the main entry keeps getting hit.
	stats := &api.CacheStats{
		Backend:         store.BackendName(retrieveResp.Store, c.bucketURL),
		TotalMs:         time.Since(startTime).Milliseconds(),
		TransferMs:      transferInfo.Duration.Milliseconds(),
		CompressedBytes: transferInfo.BytesTransferred,
		PartCount:       transferInfo.PartCount,
		Concurrency:     transferInfo.Concurrency,
	}
	confirm = func() { c.confirmRestoreSucceeded(ctx, retrieveResp, stats) }
	return file, cleanup, confirm, nil
}

// execSave saves the main entry then the output sidecar, but only if this run created the main entry or produced identical files, so output is never paired with different files from plain save or another job.
func (c *client) execSave(ctx context.Context, l logger.Logger, cacheConfig *configuration.Cache, cacheKey []api.CacheKeyPart, failOnError bool, output *archive.ArchiveInfo, recordErr error) error {
	cacheID := cacheConfig.Name
	failed := func(what string, err error) error {
		if failOnError {
			return fmt.Errorf("failed to save %s %q: %w", what, cacheID, err)
		}
		l.WithFields(
			logger.StringField("cache_id", cacheID),
			logger.StringField("error", err.Error()),
		).Warnf("Failed to save %s; continuing without failing the build", what)
		return nil
	}

	l.Infof("Saving cache: %s", cacheID)
	result, err := c.save(ctx, cacheID, cacheKey)
	if err != nil {
		return failed("cache", err)
	}
	logSaveResult(l, cacheID, result)
	mainDigest := result.Archive.Sha256Sum
	if !result.CacheEntryCreated {
		// Another run saved files at this key. This run's output describes them only if this run produced the same files.
		matches, err := archiveMatches(ctx, cacheConfig, result.ExistingDigest)
		if err != nil {
			return failed("command output for cache", err)
		}
		if !matches {
			l.Infof("Not saving command output for cache %q: its files were saved by another run and differ from this run's", cacheID)
			return nil
		}
		l.Infof("Cache %q already holds this run's files", cacheID)
		mainDigest = result.ExistingDigest
	}

	if recordErr != nil {
		return failed("command output for cache", recordErr)
	}
	l.Infof("Saving command output for cache: %s", cacheID)
	outputCtx, span := otel.Tracer("github.com/buildkite/agent/v4/internal/cache").Start(ctx, "Client.saveOutput")
	outputKey := outputCacheKey(cacheKey, mainDigest)
	result, err = c.saveEntry(outputCtx, cacheID, outputTargetPaths(cacheConfig.TargetPaths), outputKey, "zstd", false, time.Now(), SaveResult{Key: cacheID},
		func(context.Context) (*archive.ArchiveInfo, error) { return output, nil })
	span.End()
	if err != nil {
		return failed("command output for cache", err)
	}
	logSaveResult(l, cacheID, result)
	return nil
}

// archiveMatches reports whether archiving target_paths now gives digest; archives are reproducible, so equal digests mean identical files.
func archiveMatches(ctx context.Context, cacheConfig *configuration.Cache, digest string) (bool, error) {
	if digest == "" {
		return false, nil
	}
	info, err := archive.BuildArchive(ctx, cacheConfig.TargetPaths, cacheConfig.Name)
	if err != nil {
		return false, fmt.Errorf("failed to build archive to compare with the saved files: %w", err)
	}
	_ = os.Remove(info.ArchivePath)
	return info.Sha256sum == digest, nil
}

// outputCacheKey returns the sidecar key: the main entry's key plus its archive digest, so a replaced or re-saved main entry never pairs with old output.
func outputCacheKey(cacheKey []api.CacheKeyPart, mainDigest string) []api.CacheKeyPart {
	return append(slices.Clone(cacheKey), api.CacheKeyPart{Value: mainDigest, Mandatory: true})
}

// outputTargetPaths returns the target_paths addressing a cache's output sidecar.
func outputTargetPaths(targetPaths []string) []string {
	return append(slices.Clone(targetPaths), outputTargetPath)
}

// streams carries the command's output and cache exec's section headers. Headers go to stderr, so stdout holds only the command's output, and each starts on a new line even if the output didn't end with one.
type streams struct {
	stdout, stderr io.Writer
	mu             sync.Mutex
	midLine        bool // the last byte written to either stream wasn't a newline
}

func newStreams(stdout, stderr io.Writer) *streams {
	return &streams{stdout: stdout, stderr: stderr}
}

func (s *streams) Stdout() io.Writer { return lineTracker{s, s.stdout} }
func (s *streams) Stderr() io.Writer { return lineTracker{s, s.stderr} }

func (s *streams) header(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.midLine {
		text = "\n" + text
	}
	_, _ = fmt.Fprintln(s.stderr, text)
	s.midLine = false
}

type lineTracker struct {
	s *streams
	w io.Writer
}

func (t lineTracker) Write(p []byte) (int, error) {
	n, err := t.w.Write(p)
	if n > 0 {
		t.s.mu.Lock()
		t.s.midLine = p[n-1] != '\n'
		t.s.mu.Unlock()
	}
	return n, err
}
