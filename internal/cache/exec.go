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

// RunExec runs command unless the cache cfg.Names[0] holds an exact-key result, in which case it restores target_paths and replays the recorded output; on success after a miss it saves both, and cache failures follow cfg.FailOnError.
func RunExec(ctx context.Context, l logger.Logger, apiClient *api.Client, cfg Config, stdout, stderr io.Writer, command Command) error {
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
	return c.exec(ctx, l, cfg.Names[0], cfg.FailOnError, cfg.Redact, stdout, stderr, command)
}

// RunUncached runs command without the cache because of err, a cache problem, or returns err if failOnError.
func RunUncached(l logger.Logger, failOnError bool, err error, stdout, stderr io.Writer, command Command) error {
	if failOnError {
		return err
	}
	l.Warnf("%v; running the command without caching", err)
	return command(stdout, stderr)
}

func (c *client) exec(ctx context.Context, l logger.Logger, cacheID string, failOnError bool, redact Redactor, stdout, stderr io.Writer, command Command) error {
	ctx, span := otel.Tracer("github.com/buildkite/agent/v4/internal/cache").Start(ctx, "Client.exec")
	defer span.End()
	start := time.Now()

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

	// Resolve the key before the command can change its inputs, with every part mandatory: replaying a fallback's output would misrepresent what ran.
	cacheKey, err := c.resolveCacheKey(cacheConfig)
	if err != nil {
		return runUncached(fmt.Errorf("failed to resolve cache key for %q: %w", cacheID, err))
	}
	for i := range cacheKey {
		cacheKey[i].Mandatory = true
	}

	s.header("--- :package: Restoring cache...")
	hit, err := c.execRestore(ctx, l, cacheConfig, cacheKey, failOnError, s, start)
	if err != nil || hit {
		return err
	}

	rec, err := newOutputRecorder()
	if err != nil {
		return runUncached(err)
	}
	defer func() { _ = os.Remove(rec.Path()) }()

	s.header("+++ :package: No cached result, running command")
	commandStart := time.Now()
	runErr := command(io.MultiWriter(s.Stdout(), rec.Stdout()), io.MultiWriter(s.Stderr(), rec.Stderr()))
	raw, outputErr := rec.Close(time.Since(commandStart))
	if runErr != nil {
		return runErr
	}
	s.header("--- :package: Saving cache...")

	// Redact once, after the command, so secrets it registered while running (e.g. with secret get) are caught; the raw recording stays in a temp file the job can already read, and is removed.
	var output *archive.ArchiveInfo
	if outputErr == nil {
		output, outputErr = redactRecording(ctx, raw.ArchivePath, redact)
		if output != nil {
			defer func() { _ = os.Remove(output.ArchivePath) }()
		}
	}
	return c.execSave(ctx, l, cacheConfig, cacheKey, failOnError, output, outputErr)
}

// cachedOutput is a downloaded, validated output sidecar; confirm refreshes its retention once its files are restored.
type cachedOutput struct {
	path             string
	ranFor           time.Duration // how long the command ran when its output was recorded
	cleanup, confirm func()
}

// execRestore reports a hit, restoring target_paths and replaying output, only for an exact main entry with recorded output for its archive; otherwise the command runs.
func (c *client) execRestore(ctx context.Context, l logger.Logger, cacheConfig *configuration.Cache, cacheKey []api.CacheKeyPart, failOnError bool, s *streams, start time.Time) (bool, error) {
	cacheID := cacheConfig.Name

	// Check for matching output before restore downloads or touches target_paths, so files without it (e.g. from plain save) are left alone.
	var found struct {
		checked bool
		output  *cachedOutput
		err     error
	}
	result, err := c.restore(ctx, cacheID, cacheKey, func(main api.CacheEntryRetrieveResp) bool {
		if len(main.Blobs) == 0 {
			return false
		}
		found.checked = true
		found.output, found.err = c.restoreOutput(ctx, cacheConfig, outputCacheKey(cacheKey, main.Blobs[0].Digest.Value))
		return found.output != nil
	})
	if found.output != nil {
		defer found.output.cleanup()
	}
	if found.err != nil {
		if failOnError {
			return false, fmt.Errorf("failed to restore cached output for %q: %w", cacheID, found.err)
		}
		l.Warnf("Failed to restore cached output for %q: %v; running the command", cacheID, found.err)
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
	if found.checked && found.output == nil {
		result.NotRestoredReason = "Cache not restored: no recorded command output for these files"
	}
	l.Infof("%s", restoreReport(cacheID, result, nil))
	if !result.CacheRestored || !result.CacheHit {
		return false, nil
	}

	found.output.confirm()
	ranFor := found.output.ranFor
	s.header(replayHeader(ranFor - time.Since(start)))
	if _, err := replayOutput(found.output.path, s.Stdout(), s.Stderr()); err != nil {
		// The files are restored and the output was readable before, so only writing it failed; that isn't a reason to fail the build.
		err = fmt.Errorf("failed to replay cached output for %q: %w", cacheID, err)
		if failOnError {
			return true, err
		}
		l.Warnf("%v", err)
		return true, nil
	}
	s.header(timeSaved(ranFor, time.Since(start)))
	return true, nil
}

// replayHeader opens the replayed output's log group, highlighting in its title the time the hit is saving (Buildkite renders ANSI colours in group titles).
func replayHeader(saved time.Duration) string {
	if saved <= 0 {
		return "+++ :package: Replaying output from cache (command was not run)"
	}
	return fmt.Sprintf("+++ ⚡ \x1b[1;32mcache exec saved %s\x1b[0m", roundDuration(saved))
}

// timeSaved summarises a hit, warning in yellow when restoring took longer than running the command.
func timeSaved(ranFor, took time.Duration) string {
	if ranFor > 0 && took >= ranFor {
		return fmt.Sprintf("\x1b[33m⚠\x1b[0m Restored from cache in \x1b[1m%s\x1b[0m, but running the command took only \x1b[1m%s\x1b[0m: caching it isn't saving time",
			roundDuration(took), roundDuration(ranFor))
	}
	return "\x1b[32m✔\x1b[0m Restored from cache"
}

// roundDuration rounds to milliseconds under a second, tenths of a second under a minute, and seconds above.
func roundDuration(d time.Duration) time.Duration {
	if d < time.Second {
		return d.Round(time.Millisecond)
	}
	if d < time.Minute {
		return d.Round(100 * time.Millisecond)
	}
	return d.Round(time.Second)
}

// restoreOutput downloads and validates the output sidecar; nil means a miss. Missing or unreadable blobs are invalidated and treated as a miss.
func (c *client) restoreOutput(ctx context.Context, cacheConfig *configuration.Cache, outputKey []api.CacheKeyPart) (*cachedOutput, error) {
	startTime := time.Now()

	retrieveResp, exists, err := c.retrieveEntry(ctx, outputTargetPaths(cacheConfig.TargetPaths), outputKey)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve cache: %w", err)
	}
	if !exists {
		return nil, nil
	}
	if err := validateCacheStore(retrieveResp.Store, c.bucketURL); err != nil {
		return nil, fmt.Errorf("invalid cache store configuration: %w", err)
	}

	tmpDir, file, transferInfo, err := c.downloadCache(ctx, retrieveResp, c.bucketURL)
	if err != nil {
		if errors.Is(err, store.ErrBlobNotFound) || errors.Is(err, ErrDigestMismatch) {
			slog.Warn("cached command output is missing or corrupt, treating as miss and invalidating entry",
				"cache_id", cacheConfig.Name, "err", err)
			c.invalidateStaleEntry(ctx, retrieveResp)
			return nil, nil
		}
		return nil, fmt.Errorf("failed to download cache: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(tmpDir) }

	ranFor, err := replayOutput(file, io.Discard, io.Discard)
	if err != nil {
		cleanup()
		slog.Warn("cached command output is unreadable, treating as miss and invalidating entry",
			"cache_id", cacheConfig.Name, "err", err)
		c.invalidateStaleEntry(ctx, retrieveResp)
		return nil, nil
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
	return &cachedOutput{
		path:    file,
		ranFor:  ranFor,
		cleanup: cleanup,
		confirm: func() { c.confirmRestoreSucceeded(ctx, retrieveResp, stats) },
	}, nil
}

// execSave saves the output sidecar, keyed on the main archive's digest, and then the main entry with force, or neither if the output can't be saved. Output first means whichever main entry wins a race has its output; force means a main entry without output (from plain save, or an interrupted run) is replaced instead of blocking hits until it expires.
func (c *client) execSave(ctx context.Context, l logger.Logger, cacheConfig *configuration.Cache, cacheKey []api.CacheKeyPart, failOnError bool, output *archive.ArchiveInfo, outputErr error) error {
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

	if outputErr != nil {
		return failed("cache (command output couldn't be saved)", outputErr)
	}
	if err := checkPathsExist(cacheConfig.TargetPaths); err != nil {
		return failed("cache", fmt.Errorf("invalid cache paths: %w", err))
	}
	mainArchive, err := archive.BuildArchive(ctx, cacheConfig.TargetPaths, cacheID)
	if err != nil {
		return failed("cache", fmt.Errorf("failed to build archive: %w", err))
	}
	defer func() { _ = os.Remove(mainArchive.ArchivePath) }()

	l.Infof("Saving command output for cache: %s", cacheID)
	outputCtx, span := otel.Tracer("github.com/buildkite/agent/v4/internal/cache").Start(ctx, "Client.saveOutput")
	result, err := c.saveEntry(outputCtx, cacheID, outputTargetPaths(cacheConfig.TargetPaths), outputCacheKey(cacheKey, mainArchive.Sha256sum), "zstd", false, time.Now(), SaveResult{Key: cacheID},
		func(context.Context) (*archive.ArchiveInfo, error) { return output, nil })
	span.End()
	if err != nil {
		return failed("cache (command output couldn't be saved)", err)
	}
	logSaveResult(l, cacheID, result)

	l.Infof("Saving cache: %s", cacheID)
	result, err = c.saveEntry(ctx, cacheID, cacheConfig.TargetPaths, cacheKey, c.format, true, time.Now(), SaveResult{Key: cacheID},
		func(context.Context) (*archive.ArchiveInfo, error) { return mainArchive, nil })
	if err != nil {
		return failed("cache", err)
	}
	logSaveResult(l, cacheID, result)
	return nil
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
