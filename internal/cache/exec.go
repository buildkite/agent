package cache

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
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
	if err != nil {
		return err
	}
	if c == nil {
		return fmt.Errorf("cache names not found in configuration: %s", cfg.Names[0])
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

	// A cache problem before anything was restored is a miss: run uncached unless failOnError.
	runUncached := func(err error) error {
		if failOnError {
			return err
		}
		l.Warnf("%v; running the command without caching", err)
		return command(stdout, stderr)
	}

	needles, err := redactions(ctx)
	if err != nil {
		return runUncached(fmt.Errorf("couldn't get the secrets to redact from saved output: %w", err))
	}

	// Resolve the key before the command can change its inputs, with every part mandatory: replaying a fallback's output would misrepresent what ran.
	cacheKey, err := c.resolveCacheKey(cacheConfig)
	if err != nil {
		return runUncached(fmt.Errorf("failed to resolve cache key for %q: %w", cacheID, err))
	}
	for i := range cacheKey {
		cacheKey[i].Mandatory = true
	}

	_, _ = fmt.Fprintln(stdout, "--- :package: Restoring cache...")
	hit, err := c.execRestore(ctx, l, cacheConfig, cacheKey, failOnError, stdout, stderr)
	if err != nil || hit {
		return err
	}

	rec, err := newOutputRecorder(needles)
	if err != nil {
		return runUncached(err)
	}
	defer func() { _ = os.Remove(rec.Path()) }()

	_, _ = fmt.Fprintln(stdout, "+++ :package: No cached result, running command")
	runErr := command(io.MultiWriter(stdout, rec.Stdout()), io.MultiWriter(stderr, rec.Stderr()))
	output, recordErr := rec.Close()
	if runErr != nil {
		return runErr
	}

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

	_, _ = fmt.Fprintln(stdout, "--- :package: Saving cache...")
	return c.execSave(ctx, l, cacheConfig, cacheKey, failOnError, output, recordErr)
}

// execRestore reports a hit, restoring target_paths and replaying output, only for an exact main entry with recorded output for its archive; otherwise the command runs.
func (c *client) execRestore(ctx context.Context, l logger.Logger, cacheConfig *configuration.Cache, cacheKey []api.CacheKeyPart, failOnError bool, stdout, stderr io.Writer) (bool, error) {
	cacheID := cacheConfig.Name

	// Check for matching output before restore downloads or touches target_paths, so files without it (e.g. from plain save) are left alone.
	var (
		outputFile string
		cleanup    func()
		outputErr  error
		noOutput   bool
	)
	result, err := c.restore(ctx, cacheID, cacheKey, func(main api.CacheEntryRetrieveResp) bool {
		if len(main.Blobs) == 0 {
			return false
		}
		outputFile, cleanup, outputErr = c.restoreOutput(ctx, cacheConfig, outputCacheKey(cacheKey, main.Blobs[0].Digest.Value))
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

	_, _ = fmt.Fprintln(stdout, "+++ :package: Replaying output from cache (command was not run)")
	if err := replayOutput(outputFile, stdout, stderr); err != nil {
		return true, fmt.Errorf("failed to replay cached output for %q: %w", cacheID, err)
	}
	return true, nil
}

// restoreOutput downloads and validates the output sidecar (empty path = miss, else caller runs cleanup); missing or unreadable blobs are invalidated and treated as a miss.
func (c *client) restoreOutput(ctx context.Context, cacheConfig *configuration.Cache, outputKey []api.CacheKeyPart) (path string, cleanup func(), err error) {
	startTime := time.Now()

	retrieveResp, exists, err := c.retrieveEntry(ctx, outputTargetPaths(cacheConfig.TargetPaths), outputKey)
	if err != nil {
		return "", nil, fmt.Errorf("failed to retrieve cache: %w", err)
	}
	if !exists {
		return "", nil, nil
	}
	if err := validateCacheStore(retrieveResp.Store, c.bucketURL); err != nil {
		return "", nil, fmt.Errorf("invalid cache store configuration: %w", err)
	}

	tmpDir, file, transferInfo, err := c.downloadCache(ctx, retrieveResp, c.bucketURL)
	if err != nil {
		if errors.Is(err, store.ErrBlobNotFound) || errors.Is(err, ErrDigestMismatch) {
			slog.Warn("cached command output is missing or corrupt, treating as miss and invalidating entry",
				"cache_id", cacheConfig.Name, "err", err)
			c.invalidateStaleEntry(ctx, retrieveResp)
			return "", nil, nil
		}
		return "", nil, fmt.Errorf("failed to download cache: %w", err)
	}
	cleanup = func() { _ = os.RemoveAll(tmpDir) }

	if err := replayOutput(file, io.Discard, io.Discard); err != nil {
		cleanup()
		slog.Warn("cached command output is unreadable, treating as miss and invalidating entry",
			"cache_id", cacheConfig.Name, "err", err)
		c.invalidateStaleEntry(ctx, retrieveResp)
		return "", nil, nil
	}

	// Refresh the sidecar's retention so it doesn't expire while the main entry keeps getting hit.
	c.confirmRestoreSucceeded(ctx, retrieveResp, &api.CacheStats{
		Backend:         store.BackendName(retrieveResp.Store, c.bucketURL),
		TotalMs:         time.Since(startTime).Milliseconds(),
		TransferMs:      transferInfo.Duration.Milliseconds(),
		CompressedBytes: transferInfo.BytesTransferred,
		PartCount:       transferInfo.PartCount,
		Concurrency:     transferInfo.Concurrency,
	})
	return file, cleanup, nil
}

// execSave saves the main entry then, only if this run created it, the output sidecar, so output is never paired with files from plain save or another job.
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
	if !result.CacheEntryCreated {
		l.Infof("Not saving command output for cache %q: its files were saved by another run, so this run's output may not match them", cacheID)
		return nil
	}

	if recordErr != nil {
		return failed("command output for cache", recordErr)
	}
	l.Infof("Saving command output for cache: %s", cacheID)
	outputCtx, span := otel.Tracer("github.com/buildkite/agent/v4/internal/cache").Start(ctx, "Client.saveOutput")
	outputKey := outputCacheKey(cacheKey, result.Archive.Sha256Sum)
	result, err = c.saveEntry(outputCtx, cacheID, outputTargetPaths(cacheConfig.TargetPaths), outputKey, "zstd", false, time.Now(), SaveResult{Key: cacheID},
		func(context.Context) (*archive.ArchiveInfo, error) { return output, nil })
	span.End()
	if err != nil {
		return failed("command output for cache", err)
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
