package cache

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/internal/cache/archive"
	"github.com/buildkite/agent/v4/internal/cache/store"
	"github.com/buildkite/roko"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

// Restore restores a cache from storage by ID.
//
// The function performs the following workflow:
//  1. Validates the cache configuration
//  2. Checks if the cache exists (tries exact key, then fallback keys)
//  3. Downloads the cache archive from cloud storage
//  4. Extracts files to their original paths
//  5. Cleans up temporary files
//
// If no matching cache is found (including fallback keys), the function returns
// early with CacheRestored=false. This is not an error condition.
//
// The operation respects context cancellation and will stop immediately when
// ctx is cancelled, cleaning up any temporary resources (downloaded archives).
//
// Transient API errors (429, 5xx, connection reset/timeout/EOF) on
// CacheEntryRetrieve are retried automatically per api.BreakOnNonRetryable.
// Non-retryable errors (4xx other than 429) cause the operation to fail
// immediately.
//
// Progress callbacks (if configured) are invoked at each stage with the
// following stages: "validating", "checking_exists", "downloading", "extracting",
// "complete".
//
// Returns RestoreResult with detailed metrics, or an error if the operation failed.
//
// Use RestoreResult.CacheHit to check if the exact key matched, and
// RestoreResult.FallbackUsed to check if a fallback key was used.
//
// Example:
//
//	result, err := cacheClient.Restore(ctx, "node_modules")
//	if err != nil {
//	    log.Fatalf("Cache restore failed: %v", err)
//	}
//	if !result.CacheRestored {
//	    log.Printf("Cache miss for key: %s", result.Key)
//	} else if result.FallbackUsed {
//	    log.Printf("Restored from fallback key: %s", result.Key)
//	} else {
//	    log.Printf("Cache hit: %s (%.2f MB)", result.Key, float64(result.Archive.Size)/(1024*1024))
//	}
func (c *client) Restore(ctx context.Context, cacheID string) (RestoreResult, error) {
	tracer := otel.Tracer("github.com/buildkite/agent/v4/internal/cache")
	ctx, span := tracer.Start(ctx, "Client.Restore")
	defer span.End()

	span.SetAttributes(
		attribute.String("cache.id", cacheID),
		attribute.String("cache.platform", c.platform),
	)

	startTime := time.Now()
	result := RestoreResult{}

	// Find the cache configuration
	cacheConfig, err := c.findCache(cacheID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to find cache configuration")
		return result, err
	}

	result.Key = cacheID

	cacheKey, err := c.resolveCacheKey(cacheConfig)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to resolve cache key")
		return result, fmt.Errorf("failed to resolve cache key: %w", err)
	}

	span.SetAttributes(
		attribute.String("cache.id", cacheID),
		attribute.String("cache.registry", c.registry),
		attribute.Int("cache.target_paths_count", len(cacheConfig.TargetPaths)),
		attribute.Int("cache.key_parts_count", len(cacheKey)),
	)

	c.callProgress(cacheID, "validating", "Validating cache configuration", 0, 0)

	c.callProgress(cacheID, "checking_exists", "Checking if cache exists", 0, 0)

	var (
		apiResp      *api.Response
		retrieveResp api.CacheEntryRetrieveResp
		exists       bool
	)

	// Cache restore is latency-sensitive: it runs at the start of a job and
	// blocks forward progress, so transient failures should retry quickly
	// After ~5 attempts (~3.4s wall-clock with this curve),
	// treat repeated failures as a cache miss.
	err = roko.NewRetrier(
		roko.WithMaxAttempts(5),
		roko.WithStrategy(roko.ExponentialSubsecond(500*time.Millisecond)),
		roko.WithJitter(),
	).DoWithContext(ctx, func(r *roko.Retrier) error {
		var err error
		retrieveResp, exists, apiResp, err = c.api.CacheEntryRetrieve(ctx, c.registry, api.CacheEntryRetrieveReq{
			TargetPaths: cacheConfig.TargetPaths,
			CacheKey:    cacheKey,
		})
		if api.BreakOnNonRetryable(r, apiResp, err) {
			return err
		}
		if err != nil {
			slog.Warn("cache retrieve failed, retrying", "err", err, "retrier", r.String())
			return err
		}
		return nil
	})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to retrieve cache")
		return result, fmt.Errorf("failed to retrieve cache: %w", err)
	}

	if !exists {
		// Cache miss
		result.CacheHit = false
		result.CacheRestored = false
		result.TotalDuration = time.Since(startTime)
		span.SetAttributes(
			attribute.Bool("cache.hit", false),
			attribute.Bool("cache.restored", false),
			attribute.Int64("cache.duration_ms", result.TotalDuration.Milliseconds()),
		)
		span.SetStatus(codes.Ok, "cache miss")
		c.callProgress(cacheID, "complete", "Cache miss", 0, 0)
		return result, nil
	}

	// Cache found (either exact match or fallback)
	result.FallbackUsed = retrieveResp.Fallback
	result.CacheHit = !retrieveResp.Fallback
	result.ExpiresAt = retrieveResp.ExpiresAt

	span.SetAttributes(
		attribute.Bool("cache.fallback_used", result.FallbackUsed),
		attribute.String("cache.matched_key", result.Key),
	)

	// Validate the cache store configuration (e.g. BUILDKITE_AGENT_CACHE_STORE_URL
	// is set for the S3 store) before attempting a download.
	if err := validateCacheStore(retrieveResp.Store, c.bucketURL); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "invalid cache store configuration")
		return result, fmt.Errorf("invalid cache store configuration: %w", err)
	}

	c.callProgress(cacheID, "downloading", "Downloading cache archive", 0, 0)

	// Download cache
	tmpDir, archiveFile, transferInfo, err := c.downloadCache(ctx, retrieveResp, c.bucketURL)
	if err != nil {
		if errors.Is(err, store.ErrBlobNotFound) {
			slog.Warn("cache blob missing, treating as miss and invalidating entry",
				"cache_id", cacheID, "err", err)
			invalidated := c.invalidateStaleEntry(ctx, retrieveResp)
			// The blob is gone, so nothing was restored: clear the hit/fallback
			// state set earlier so callers and the span reflect a clean miss.
			result.CacheHit = false
			result.FallbackUsed = false
			result.CacheRestored = false
			result.TotalDuration = time.Since(startTime)
			span.SetAttributes(
				attribute.Bool("cache.hit", false),
				attribute.Bool("cache.restored", false),
				attribute.Bool("cache.invalidated", invalidated),
			)
			span.SetStatus(codes.Ok, "cache miss (missing blob)")
			c.callProgress(cacheID, "complete", missCompleteMessage("missing blob", invalidated), 0, 0)
			return result, nil
		}
		if errors.Is(err, ErrDigestMismatch) {
			// The blob doesn't match its fingerprint. downloadCache verified this
			// before any target folder was touched, so existing files are intact.
			// The blob will never verify, so expire the entry to let a subsequent
			// save re-upload it, and let the build continue with a clean miss.
			slog.Warn("cache blob digest mismatch, treating as miss and invalidating entry",
				"cache_id", cacheID, "err", err)
			invalidated := c.invalidateStaleEntry(ctx, retrieveResp)
			result.CacheHit = false
			result.FallbackUsed = false
			result.CacheRestored = false
			result.TotalDuration = time.Since(startTime)
			span.SetAttributes(
				attribute.Bool("cache.hit", false),
				attribute.Bool("cache.restored", false),
				attribute.Bool("cache.digest_mismatch", true),
				attribute.Bool("cache.invalidated", invalidated),
			)
			span.SetStatus(codes.Ok, "cache miss (digest mismatch)")
			c.callProgress(cacheID, "complete", missCompleteMessage("blob digest mismatch", invalidated), 0, 0)
			return result, nil
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to download cache")
		return result, fmt.Errorf("failed to download cache: %w", err)
	}
	defer func() {
		_ = os.RemoveAll(tmpDir)
	}()

	// Populate transfer metrics
	result.Transfer = TransferMetrics{
		BytesTransferred: transferInfo.BytesTransferred,
		TransferSpeed:    transferInfo.TransferSpeed,
		Duration:         transferInfo.Duration,
		RequestID:        transferInfo.RequestID,
		PartCount:        transferInfo.PartCount,
		Concurrency:      transferInfo.Concurrency,
	}

	// Check the archive is a readable format before touching the filesystem.
	if err := archive.Validate(archiveFile, transferInfo.BytesTransferred); err != nil {
		// A bad cache must never block a build: an unrecognized or otherwise
		// unreadable archive degrades to a miss and invalidates the entry
		// (targets untouched — this runs before cleaning).
		unrecognized := errors.Is(err, archive.ErrUnrecognizedFormat)
		slog.Warn("cache archive failed verification, treating as miss and invalidating entry",
			"cache_id", cacheID, "unrecognized_format", unrecognized, "err", err)
		invalidated := c.invalidateStaleEntry(ctx, retrieveResp)
		result.CacheHit = false
		result.FallbackUsed = false
		result.CacheRestored = false
		result.TotalDuration = time.Since(startTime)
		span.SetAttributes(
			attribute.Bool("cache.hit", false),
			attribute.Bool("cache.restored", false),
			attribute.Bool("cache.unrecognized_format", unrecognized),
			attribute.Bool("cache.invalidated", invalidated),
		)
		span.SetStatus(codes.Ok, "cache miss (archive failed verification)")
		c.callProgress(cacheID, "complete", missCompleteMessage("archive failed verification", invalidated), 0, 0)
		return result, nil
	}

	// Portable anchors (~, .) re-resolve here, so targets that didn't overlap at
	// save can converge (e.g. "~/cache" and "/home/b/cache" when HOME=/home/b).
	// Re-check the resolved paths and soft-fail before touching the filesystem.
	resolvedTargets := make([]string, len(cacheConfig.TargetPaths))
	for i, path := range cacheConfig.TargetPaths {
		resolved, err := archive.ResolveConfigPath(path)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "failed to resolve target path")
			return result, fmt.Errorf("failed to resolve target path %q: %w", path, err)
		}
		resolvedTargets[i] = resolved
	}
	if a, b, ok := archive.OverlappingPaths(resolvedTargets); ok {
		slog.Warn("configured cache targets overlap after re-resolving anchors, treating as miss",
			"cache_id", cacheID, "target_a", cacheConfig.TargetPaths[a], "target_b", cacheConfig.TargetPaths[b])
		result.CacheHit = false
		result.FallbackUsed = false
		result.CacheRestored = false
		result.TotalDuration = time.Since(startTime)
		span.SetAttributes(
			attribute.Bool("cache.hit", false),
			attribute.Bool("cache.restored", false),
			attribute.Bool("cache.targets_overlap", true),
		)
		span.SetStatus(codes.Ok, "cache miss (targets overlap at restore)")
		c.callProgress(cacheID, "complete", "Cache miss (targets overlap at restore)", 0, 0)
		return result, nil
	}

	c.callProgress(cacheID, "cleaning", "Cleaning paths", 0, 0)

	for _, path := range cacheConfig.TargetPaths {
		// Resolve exactly as save/restore matching does.
		extractedPath, err := archive.ResolveConfigPath(path)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "failed to resolve target path")
			return result, fmt.Errorf("failed to resolve target path %q: %w", path, err)
		}

		slog.Debug("cleaning path", "path", path, "extractedPath", extractedPath)

		if err := cleanPath(ctx, extractedPath); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "failed to clean path")
			return result, fmt.Errorf("failed to clean path %q: %w", extractedPath, err)
		}
	}

	c.callProgress(cacheID, "extracting", "Extracting files from cache", 0, int(transferInfo.BytesTransferred))

	// Extract files
	archiveInfo, err := c.extractCache(ctx, archiveFile, transferInfo.BytesTransferred, cacheConfig.TargetPaths)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to extract cache")
		return result, fmt.Errorf("failed to extract cache: %w", err)
	}

	// Populate archive metrics
	result.Archive = ArchiveMetrics{
		Size:             archiveInfo.Size,
		WrittenBytes:     archiveInfo.WrittenBytes,
		WrittenEntries:   archiveInfo.WrittenEntries,
		CompressionRatio: float64(archiveInfo.WrittenBytes) / float64(archiveInfo.Size),
		Duration:         archiveInfo.Duration,
		Paths:            cacheConfig.TargetPaths,
	}

	result.CacheRestored = true
	result.TotalDuration = time.Since(startTime)

	// The restore is now fully verified (downloaded, digest-checked, and
	// extracted) — confirm it with the server so retention refreshes off a
	// restore that's actually known to be good.
	confirmed := c.confirmRestoreSucceeded(ctx, retrieveResp)

	// Add result attributes to span
	span.SetAttributes(
		attribute.Bool("cache.hit", result.CacheHit),
		attribute.Bool("cache.restored", result.CacheRestored),
		attribute.Bool("cache.confirmed", confirmed),
		attribute.Int64("cache.archive_size_bytes", result.Archive.Size),
		attribute.Int64("cache.written_bytes", result.Archive.WrittenBytes),
		attribute.Int64("cache.written_entries", result.Archive.WrittenEntries),
		attribute.Float64("cache.compression_ratio", result.Archive.CompressionRatio),
		attribute.Int64("cache.transfer_bytes", result.Transfer.BytesTransferred),
		attribute.Float64("cache.transfer_speed_mbps", result.Transfer.TransferSpeed),
		attribute.Int64("cache.duration_ms", result.TotalDuration.Milliseconds()),
	)
	span.SetStatus(codes.Ok, "cache restored successfully")

	c.callProgress(cacheID, "complete", "Cache restored successfully", 0, 0)

	return result, nil
}

// invalidateStaleEntry uses the retrieve response to expire a cache entry whose
// blob is missing or fails digest verification, so a subsequent save re-uploads it.
// Returns whether an entry was actually deleted — a successful (2xx) call can
// still be an idempotent no-op, which must not be reported as invalidated.
func (c *client) invalidateStaleEntry(ctx context.Context, retrieveResp api.CacheEntryRetrieveResp) bool {
	if len(retrieveResp.TargetPaths) == 0 || len(retrieveResp.CacheKey) == 0 {
		slog.Warn("cannot invalidate stale cache entry: retrieve response missing resolved address")
		return false
	}

	req := api.CacheEntryExpireReq{
		TargetPaths: retrieveResp.TargetPaths,
		CacheKey:    retrieveResp.CacheKey,
		Scopes:      retrieveResp.Scopes,
	}
	var existed bool
	err := roko.NewRetrier(
		roko.WithMaxAttempts(5),
		roko.WithStrategy(roko.ExponentialSubsecond(500*time.Millisecond)),
		roko.WithJitter(),
	).DoWithContext(ctx, func(r *roko.Retrier) error {
		expireResp, apiResp, err := c.api.CacheEntryExpire(ctx, c.registry, req)
		if api.BreakOnNonRetryable(r, apiResp, err) {
			return err
		}
		if err != nil {
			slog.Warn("cache entry invalidation failed, retrying", "err", err, "retrier", r.String())
			return err
		}
		existed = expireResp.Existed
		return nil
	})
	if err != nil {
		slog.Warn("cache entry invalidation failed", "registry", c.registry, "err", err)
		return false
	}
	return existed
}

// confirmRestoreSucceeded tells the server this restore's blob was verified,
// so the registry can safely refresh the entry's retention now rather than
// when retrieve first served it, before the blob was known to be good.
//
// Skipped for a fallback match — mirrors the backend's own (now-removed)
// `unless entry_result.fallback_used?` bump guard: a fallback hit means the
// entry was saved under a shorter key sequence, and refreshing it would
// reset the clock on an entry the caller didn't explicitly target.
//
// Best-effort: never fails the build.
func (c *client) confirmRestoreSucceeded(ctx context.Context, retrieveResp api.CacheEntryRetrieveResp) bool {
	if retrieveResp.Fallback {
		return false
	}
	if len(retrieveResp.TargetPaths) == 0 || len(retrieveResp.CacheKey) == 0 {
		slog.Warn("cannot confirm cache restore: retrieve response missing resolved address")
		return false
	}

	req := api.CacheEntryConfirmReq{
		TargetPaths: retrieveResp.TargetPaths,
		CacheKey:    retrieveResp.CacheKey,
		Scopes:      retrieveResp.Scopes,
	}
	err := roko.NewRetrier(
		roko.WithMaxAttempts(5),
		roko.WithStrategy(roko.ExponentialSubsecond(500*time.Millisecond)),
		roko.WithJitter(),
	).DoWithContext(ctx, func(r *roko.Retrier) error {
		_, apiResp, err := c.api.CacheEntryConfirm(ctx, c.registry, req)
		if api.BreakOnNonRetryable(r, apiResp, err) {
			return err
		}
		if err != nil {
			slog.Warn("cache restore confirmation failed, retrying", "err", err, "retrier", r.String())
			return err
		}
		return nil
	})
	if err != nil {
		slog.Warn("cache restore confirmation failed", "registry", c.registry, "err", err)
		return false
	}
	return true
}

// missCompleteMessage builds the progress text for a stale-entry miss,
// distinguishing an actual deletion from a no-op so callers aren't told an
// entry was invalidated when it wasn't.
func missCompleteMessage(reason string, invalidated bool) string {
	if invalidated {
		return fmt.Sprintf("Cache miss (%s, invalidated stale entry)", reason)
	}
	return fmt.Sprintf("Cache miss (%s, stale entry could not be invalidated)", reason)
}

// downloadCache downloads a cache archive from storage
func (c *client) downloadCache(ctx context.Context, retrieveResp api.CacheEntryRetrieveResp, bucketURL string) (tmpDir, archiveFile string, transferInfo *store.TransferInfo, err error) {
	tracer := otel.Tracer("github.com/buildkite/agent/v4/internal/cache")
	ctx, span := tracer.Start(ctx, "Client.downloadCache")
	defer span.End()

	// Content-addressed storage: the object name is the blob digest.
	//
	//Currently, a save always produces a single archive, so an entry has exactly one
	// blob and we download blobs[0]. The blobs field is an array to leave room
	// for content-defined chunking later (one entry → many chunk-blobs)
	if len(retrieveResp.Blobs) == 0 {
		return "", "", nil, fmt.Errorf("cache entry has no blobs to download")
	}
	storeObjectName := retrieveResp.Blobs[0].Digest.Value

	span.SetAttributes(
		attribute.String("cache.store_type", retrieveResp.Store),
		attribute.String("cache.object_name", storeObjectName),
	)

	// Create blob store
	blobStore, err := store.NewBlobStore(ctx, retrieveResp.Store, bucketURL)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to create blob store")
		return "", "", nil, fmt.Errorf("failed to create blob store: %w", err)
	}

	// Create temporary directory
	tmpDir, err = os.MkdirTemp("", "zstash-restore")
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to create temp directory")
		return "", "", nil, fmt.Errorf("failed to create temp directory: %w", err)
	}

	archiveFile = filepath.Join(tmpDir, storeObjectName)

	// Download archive
	transferInfo, err = blobStore.Download(ctx, storeObjectName, archiveFile)
	if err != nil {
		// Clean up temporary directory on failure
		_ = os.RemoveAll(tmpDir)
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to download from blob store")
		return "", "", nil, fmt.Errorf("failed to download cache: %w", err)
	}

	span.SetAttributes(
		attribute.Int64("cache.bytes_transferred", transferInfo.BytesTransferred),
		attribute.Float64("cache.transfer_speed_mbps", transferInfo.TransferSpeed),
		attribute.String("cache.request_id", transferInfo.RequestID),
	)

	// Verify the download matches its content-addressed name before returning it
	// to the caller, so a bad blob never reaches the target folders.
	if err := verifyBlobDigest(ctx, archiveFile, retrieveResp.Blobs[0].Digest); err != nil {
		_ = os.RemoveAll(tmpDir)
		span.RecordError(err)
		if errors.Is(err, ErrDigestMismatch) {
			span.SetAttributes(attribute.Bool("cache.digest_mismatch", true))
		}
		span.SetStatus(codes.Error, "cache blob digest verification failed")
		return "", "", nil, err
	}

	// Extend the blob's retention now that it's confirmed good.
	maybeRefreshRetention(ctx, blobStore, retrieveResp.Fallback, storeObjectName)

	span.SetStatus(codes.Ok, "download completed")

	return tmpDir, archiveFile, transferInfo, nil
}

// maybeRefreshRetention extends key's retention via blobStore, unless
// fallback is true — a fallback hit means the entry was saved under a
// shorter key sequence, and refreshing it would reset the clock on a blob
// the caller didn't explicitly target (mirrors the backend's
// `unless entry_result.fallback_used?` TTL-bump guard).
func maybeRefreshRetention(ctx context.Context, blobStore store.Blob, fallback bool, key string) {
	refresher, ok := blobStore.(store.RetentionRefresher)
	if !ok || fallback {
		return
	}
	refresher.RefreshRetention(ctx, key)
}

// extractCache extracts files from a cache archive
func (c *client) extractCache(ctx context.Context, archiveFile string, archiveSize int64, paths []string) (*archive.ArchiveInfo, error) {
	tracer := otel.Tracer("github.com/buildkite/agent/v4/internal/cache")
	ctx, span := tracer.Start(ctx, "Client.extractCache")
	defer span.End()

	span.SetAttributes(
		attribute.String("cache.archive_file", archiveFile),
		attribute.Int64("cache.archive_size_bytes", archiveSize),
		attribute.Int("cache.paths_count", len(paths)),
	)

	// Open archive file
	archiveFileHandle, err := os.Open(archiveFile)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to open archive file")
		return nil, fmt.Errorf("failed to open archive file: %w", err)
	}
	defer func() { _ = archiveFileHandle.Close() }()

	// Extract files
	archiveInfo, err := archive.ExtractFiles(ctx, archiveFileHandle, archiveSize, paths)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to extract archive")
		return nil, fmt.Errorf("failed to extract archive: %w", err)
	}

	span.SetAttributes(
		attribute.Int64("cache.written_bytes", archiveInfo.WrittenBytes),
		attribute.Int64("cache.written_entries", archiveInfo.WrittenEntries),
	)
	span.SetStatus(codes.Ok, "extraction completed")

	return archiveInfo, nil
}

// cleanPath removes a directory tree for a configured cache path.
// It handles Go module cache directories that have 0555 permissions by
// making them writable before removal.
func cleanPath(ctx context.Context, dir string) error {
	if dir == "" {
		return fmt.Errorf("cleanPath: empty directory path")
	}

	clean := filepath.Clean(dir)

	// Cheap lexical guards for degenerate spellings.
	if clean == "." || clean == string(os.PathSeparator) {
		return fmt.Errorf("cleanPath: refusing to remove %q", clean)
	}
	if runtime.GOOS == "windows" {
		if len(clean) == 3 && clean[1] == ':' && clean[2] == '\\' {
			return fmt.Errorf("cleanPath: refusing to remove drive root %q", clean)
		}
		// UNC share root (\\server\share): filepath.Clean leaves it equal to the
		// volume name with no trailing separator, so the drive-root check above
		// misses it — reject it so cleanup never recurses the whole share.
		if vol := filepath.VolumeName(clean); vol != "" && clean == vol {
			return fmt.Errorf("cleanPath: refusing to remove volume root %q", clean)
		}
	}
	// A final symlink is only unlinked by RemoveAll, so the guards below run only
	// for a real (non-symlink) target; os.Lstat doesn't dereference it.
	if lstat, err := os.Lstat(clean); err == nil && lstat.Mode()&os.ModeSymlink == 0 {
		// Refuse if the target is, or is an ancestor of, a protected dir (cwd,
		// home, root) — removing it would delete that dir too. By file identity,
		// so symlinked parents and case variants count, not just an exact match.
		for _, p := range protectedDirs() {
			if pathContains(lstat, p) {
				return fmt.Errorf("cleanPath: refusing to remove %q (it is or contains protected directory %q)", clean, p)
			}
		}

		// Module cache has 0555 directories; make them writable before removal.
		if lstat.IsDir() {
			if err := makeTreeWritable(ctx, clean); err != nil {
				return err
			}
		}
	}

	// Check context again before potentially long RemoveAll
	if ctx.Err() != nil {
		return ctx.Err()
	}

	if err := os.RemoveAll(clean); err != nil {
		return fmt.Errorf("cleanPath: failed to remove %q: %w", clean, err)
	}

	return nil
}

// protectedDirs returns directories cleanPath must never remove: the filesystem
// root, the working directory, and the user's home directory.
func protectedDirs() []string {
	dirs := []string{string(os.PathSeparator)}
	if cwd, err := os.Getwd(); err == nil {
		dirs = append(dirs, cwd)
	}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, home)
	}
	return dirs
}

// pathContains reports whether the file described by info is dir itself or an
// ancestor of it.
func pathContains(info os.FileInfo, dir string) bool {
	for cur := filepath.Clean(dir); ; {
		if ci, err := os.Stat(cur); err == nil && os.SameFile(info, ci) {
			return true
		}
		parent := filepath.Dir(cur)
		if parent == cur { // reached the root
			return false
		}
		cur = parent
	}
}

// makeTreeWritable walks `clean` and chmods every directory to 0755 so that
// the subsequent os.RemoveAll can delete read-only entries (e.g. Go module
// cache). The os.Root handle is closed before returning so that the caller
// can remove `clean` on platforms (Windows) that disallow removing a
// directory with an open handle.
func makeTreeWritable(ctx context.Context, clean string) error {
	root, err := os.OpenRoot(clean)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("cleanPath: open root %q: %w", clean, err)
	}
	defer func() { _ = root.Close() }()

	err = fs.WalkDir(root.FS(), ".", func(relPath string, info fs.DirEntry, walkErr error) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if walkErr != nil {
			slog.Debug("cleanPath: error walking path", "path", relPath, "err", walkErr)
			return nil
		}

		if info.IsDir() {
			if chmodErr := root.Chmod(relPath, 0o755); chmodErr != nil {
				return fmt.Errorf("chmod %q: %w", relPath, chmodErr)
			}
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return fmt.Errorf("cleanPath: error preparing %q for removal: %w", clean, err)
	}
	return nil
}
