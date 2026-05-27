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
	"strings"
	"time"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/internal/cache/archive"
	"github.com/buildkite/agent/v3/internal/cache/store"
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
	tracer := otel.Tracer("github.com/buildkite/agent/v3/internal/cache")
	ctx, span := tracer.Start(ctx, "Client.Restore")
	defer span.End()

	span.SetAttributes(
		attribute.String("cache.id", cacheID),
		attribute.String("cache.branch", c.branch),
		attribute.String("cache.pipeline", c.pipeline),
		attribute.String("cache.organization", c.organization),
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

	result.Key = cacheConfig.Key

	span.SetAttributes(
		attribute.String("cache.key", cacheConfig.Key),
		attribute.String("cache.registry", c.registry),
		attribute.StringSlice("cache.fallback_keys", cacheConfig.FallbackKeys),
		attribute.Int("cache.paths_count", len(cacheConfig.Paths)),
	)

	c.callProgress(cacheID, "validating", "Validating cache configuration", 0, 0)

	c.callProgress(cacheID, "checking_exists", "Checking if cache exists", 0, 0)

	// Check if cache exists
	retrieveResp, exists, err := c.api.CacheEntryRetrieve(ctx, c.registry, api.CacheEntryRetrieveReq{
		Key:          cacheConfig.Key,
		Branch:       c.branch,
		FallbackKeys: strings.Join(cacheConfig.FallbackKeys, ","),
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
	result.Key = retrieveResp.Key
	result.FallbackUsed = retrieveResp.Fallback
	result.CacheHit = !retrieveResp.Fallback
	result.ExpiresAt = retrieveResp.ExpiresAt

	span.SetAttributes(
		attribute.Bool("cache.fallback_used", result.FallbackUsed),
		attribute.String("cache.matched_key", result.Key),
	)

	c.callProgress(cacheID, "downloading", "Downloading cache archive", 0, 0)

	// Download cache
	tmpDir, archiveFile, transferInfo, err := c.downloadCache(ctx, retrieveResp, c.bucketURL)
	if err != nil {
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

	c.callProgress(cacheID, "cleaning", "Cleaning paths", 0, 0)

	for _, path := range cacheConfig.Paths {
		extractedPath, err := archive.ResolveHomeDir(path)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "failed to resolve home dir")
			return result, fmt.Errorf("failed to resolve home dir for %q: %w", path, err)
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
	archiveInfo, err := c.extractCache(ctx, archiveFile, transferInfo.BytesTransferred, cacheConfig.Paths)
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
		Paths:            cacheConfig.Paths,
	}

	result.CacheRestored = true
	result.TotalDuration = time.Since(startTime)

	// Add result attributes to span
	span.SetAttributes(
		attribute.Bool("cache.hit", result.CacheHit),
		attribute.Bool("cache.restored", result.CacheRestored),
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

// downloadCache downloads a cache archive from storage
func (c *client) downloadCache(ctx context.Context, retrieveResp api.CacheEntryRetrieveResp, bucketURL string) (tmpDir, archiveFile string, transferInfo *store.TransferInfo, err error) {
	tracer := otel.Tracer("github.com/buildkite/agent/v3/internal/cache")
	ctx, span := tracer.Start(ctx, "Client.downloadCache")
	defer span.End()

	span.SetAttributes(
		attribute.String("cache.store_type", retrieveResp.Store),
		attribute.String("cache.object_name", retrieveResp.StoreObjectName),
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

	archiveFile = filepath.Join(tmpDir, retrieveResp.StoreObjectName)

	// Download archive
	transferInfo, err = blobStore.Download(ctx, retrieveResp.StoreObjectName, archiveFile)
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
	span.SetStatus(codes.Ok, "download completed")

	return tmpDir, archiveFile, transferInfo, nil
}

// extractCache extracts files from a cache archive
func (c *client) extractCache(ctx context.Context, archiveFile string, archiveSize int64, paths []string) (*archive.ArchiveInfo, error) {
	tracer := otel.Tracer("github.com/buildkite/agent/v3/internal/cache")
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

	// Refuse to delete root or current directory
	if clean == "." || clean == string(os.PathSeparator) {
		return fmt.Errorf("cleanPath: refusing to remove %q", clean)
	}

	// On Windows, also check for drive roots like "C:\"
	if runtime.GOOS == "windows" && len(clean) == 3 && clean[1] == ':' && clean[2] == '\\' {
		return fmt.Errorf("cleanPath: refusing to remove drive root %q", clean)
	}

	// Refuse to delete home directory
	if home, err := os.UserHomeDir(); err == nil {
		if clean == filepath.Clean(home) {
			return fmt.Errorf("cleanPath: refusing to remove home directory %q", clean)
		}
	}

	// Module cache has 0555 directories; make them writable in order to remove content.
	if err := makeTreeWritable(ctx, clean); err != nil {
		return err
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
