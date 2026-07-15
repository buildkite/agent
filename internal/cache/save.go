package cache

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/internal/cache/archive"
	"github.com/buildkite/agent/v3/internal/cache/store"
	"github.com/buildkite/roko"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

// Save saves a cache to storage by ID.
//
// The function performs the following workflow:
//  1. Validates the cache configuration and paths exist
//  2. Checks if the cache already exists (early return if yes)
//  3. Builds an archive of the cache paths
//  4. Creates a cache entry in the Buildkite API
//  5. Uploads the archive to cloud storage
//  6. Commits the cache entry
//
// If the cache already exists, no upload is performed and the function returns
// early with CacheEntryCreated=false and Transfer=nil.
//
// The operation respects context cancellation and will stop immediately when
// ctx is cancelled, cleaning up any temporary resources.
//
// Transient API errors (429, 5xx, connection reset/timeout/EOF) on
// CacheEntryPeekExists, CacheRegistry, CacheEntryCreate, and CacheEntryCommit
// are retried automatically per api.BreakOnNonRetryable. Non-retryable errors
// (4xx other than 429) cause the operation to fail immediately.
//
// Progress callbacks (if configured) are invoked at each stage with the
// following stages: "validating", "checking_exists", "fetching_registry",
// "building_archive", "creating_entry", "uploading", "committing", "complete".
//
// Returns SaveResult with detailed metrics, or an error if the operation failed.
//
// Example:
//
//	result, err := cacheClient.Save(ctx, "node_modules")
//	if err != nil {
//	    log.Fatalf("Cache save failed: %v", err)
//	}
//	if !result.CacheEntryCreated {
//	    log.Printf("Cache already exists for key: %s", result.Key)
//	} else {
//	    log.Printf("Cache saved: %s (%.2f MB)", result.Key, float64(result.Archive.Size)/(1024*1024))
//	}
func (c *client) Save(ctx context.Context, cacheID string) (SaveResult, error) {
	tracer := otel.Tracer("github.com/buildkite/agent/v3/internal/cache")
	ctx, span := tracer.Start(ctx, "Client.Save")
	defer span.End()

	span.SetAttributes(
		attribute.String("cache.id", cacheID),
		attribute.String("cache.platform", c.platform),
		attribute.String("cache.format", c.format),
	)

	startTime := time.Now()
	result := SaveResult{}

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

	// Validate cache paths exist
	if err := checkPathsExist(cacheConfig.TargetPaths); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "invalid cache paths")
		return result, fmt.Errorf("invalid cache paths: %w", err)
	}

	c.callProgress(cacheID, "checking_exists", "Checking if cache already exists", 0, 0)

	// Check if cache already exists
	var (
		peekApiResp *api.Response
		exists      bool
	)

	err = roko.NewRetrier(
		roko.WithMaxAttempts(5),
		roko.WithStrategy(roko.ExponentialSubsecond(500*time.Millisecond)),
		roko.WithJitter(),
	).DoWithContext(ctx, func(r *roko.Retrier) error {
		var err error
		_, exists, peekApiResp, err = c.api.CacheEntryPeekExists(ctx, c.registry, api.CacheEntryPeekReq{
			TargetPaths: cacheConfig.TargetPaths,
			CacheKey:    cacheKey,
		})
		if api.BreakOnNonRetryable(r, peekApiResp, err) {
			return err
		}
		if err != nil {
			slog.Warn("cache peek failed, retrying", "err", err, "retrier", r.String())
			return err
		}
		return nil
	})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to check cache existence")
		return result, fmt.Errorf("failed to check cache existence: %w", err)
	}

	if exists {
		// Cache already exists, no need to upload
		result.CacheEntryCreated = false
		result.TotalDuration = time.Since(startTime)
		span.SetAttributes(
			attribute.Bool("cache.created", false),
			attribute.Bool("cache.already_exists", true),
			attribute.Int64("cache.duration_ms", result.TotalDuration.Milliseconds()),
		)
		span.SetStatus(codes.Ok, "cache already exists")
		c.callProgress(cacheID, "complete", "Cache already exists", 0, 0)
		return result, nil
	}

	c.callProgress(cacheID, "fetching_registry", "Looking up cache registry", 0, 0)

	// Get cache registry information
	var (
		registryApiResp *api.Response
		registryResp    api.CacheRegistryResp
	)

	err = roko.NewRetrier(
		roko.WithMaxAttempts(5),
		roko.WithStrategy(roko.ExponentialSubsecond(500*time.Millisecond)),
		roko.WithJitter(),
	).DoWithContext(ctx, func(r *roko.Retrier) error {
		var err error
		registryResp, registryApiResp, err = c.api.CacheRegistry(ctx, c.registry)
		if api.BreakOnNonRetryable(r, registryApiResp, err) {
			return err
		}
		if err != nil {
			slog.Warn("cache registry lookup failed, retrying", "err", err, "retrier", r.String())
			return err
		}
		return nil
	})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to get cache registry")
		return result, fmt.Errorf("failed to get cache registry: %w", err)
	}

	span.SetAttributes(
		attribute.String("cache.store_type", registryResp.Store),
	)

	// Validate cache store configuration
	if err := validateCacheStore(registryResp.Store, c.bucketURL); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "invalid cache store configuration")
		return result, fmt.Errorf("invalid cache store configuration: %w", err)
	}

	c.callProgress(cacheID, "building_archive", "Building archive", 0, len(cacheConfig.TargetPaths))

	// Build archive
	archiveInfo, err := archive.BuildArchive(ctx, cacheConfig.TargetPaths, cacheID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to build archive")
		return result, fmt.Errorf("failed to build archive: %w", err)
	}
	defer func() {
		_ = os.Remove(archiveInfo.ArchivePath)
	}()

	// Populate archive metrics
	result.Archive = ArchiveMetrics{
		Size:             archiveInfo.Size,
		WrittenBytes:     archiveInfo.WrittenBytes,
		WrittenEntries:   archiveInfo.WrittenEntries,
		CompressionRatio: float64(archiveInfo.WrittenBytes) / float64(archiveInfo.Size),
		Sha256Sum:        archiveInfo.Sha256sum,
		Duration:         archiveInfo.Duration,
		Paths:            cacheConfig.TargetPaths,
	}

	span.SetAttributes(
		attribute.Int64("cache.archive_size_bytes", archiveInfo.Size),
		attribute.Int64("cache.written_bytes", archiveInfo.WrittenBytes),
		attribute.Int64("cache.written_entries", archiveInfo.WrittenEntries),
		attribute.Float64("cache.compression_ratio", result.Archive.CompressionRatio),
		attribute.String("cache.sha256sum", archiveInfo.Sha256sum),
	)

	c.callProgress(cacheID, "creating_entry", "Creating cache entry", 0, 0)

	// Create cache entry.
	// Retry-safe: server generates a fresh upload_uuid per call; orphaned temp
	// rows + presigned URLs self-expire.
	var (
		createApiResp *api.Response
		createResp    api.CacheEntryCreateResp
	)

	err = roko.NewRetrier(
		roko.WithMaxAttempts(5),
		roko.WithStrategy(roko.ExponentialSubsecond(500*time.Millisecond)),
		roko.WithJitter(),
	).DoWithContext(ctx, func(r *roko.Retrier) error {
		var err error
		createResp, createApiResp, err = c.api.CacheEntryCreate(ctx, c.registry, api.CacheEntryCreateReq{
			TargetPaths: cacheConfig.TargetPaths,
			CacheKey:    cacheKey,
			Blobs: []api.CacheBlob{{
				Digest:      api.CacheDigest{Algorithm: "sha256", Value: archiveInfo.Sha256sum},
				FileSize:    archiveInfo.Size,
				Compression: c.format,
			}},
			Platform: c.platform,
		})
		if api.BreakOnNonRetryable(r, createApiResp, err) {
			return err
		}
		if err != nil {
			slog.Warn("cache entry create failed, retrying", "err", err, "retrier", r.String())
			return err
		}
		return nil
	})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to create cache entry")
		return result, fmt.Errorf("failed to create cache entry: %w", err)
	}

	result.UploadID = createResp.UploadID

	// Content-addressed storage: the object name is the archive's digest, which
	// the agent derives itself (the backend no longer echoes a store object name).
	storeObjectName := archiveInfo.Sha256sum

	span.SetAttributes(
		attribute.String("cache.upload_id", createResp.UploadID),
		attribute.String("cache.object_name", storeObjectName),
	)

	c.callProgress(cacheID, "uploading", "Uploading cache archive", 0, int(archiveInfo.Size))

	// Upload archive
	blobStore, err := store.NewBlobStore(ctx, registryResp.Store, c.bucketURL)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to create blob store")
		return result, fmt.Errorf("failed to create blob store: %w", err)
	}

	transferInfo, err := blobStore.Upload(ctx, archiveInfo.ArchivePath, storeObjectName)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to upload cache")
		return result, fmt.Errorf("failed to upload cache: %w", err)
	}

	// Populate transfer metrics
	result.Transfer = &TransferMetrics{
		BytesTransferred: transferInfo.BytesTransferred,
		TransferSpeed:    transferInfo.TransferSpeed,
		Duration:         transferInfo.Duration,
		RequestID:        transferInfo.RequestID,
		PartCount:        transferInfo.PartCount,
		Concurrency:      transferInfo.Concurrency,
	}

	span.SetAttributes(
		attribute.Int64("cache.transfer_bytes", transferInfo.BytesTransferred),
		attribute.Float64("cache.transfer_speed_mbps", transferInfo.TransferSpeed),
		attribute.String("cache.request_id", transferInfo.RequestID),
	)

	c.callProgress(cacheID, "committing", "Committing cache entry", 0, 0)

	// Commit cache.
	// Retry-safe: server-side put_item is an unconditional overwrite with
	// deterministic content.
	var commitApiResp *api.Response

	err = roko.NewRetrier(
		roko.WithMaxAttempts(5),
		roko.WithStrategy(roko.ExponentialSubsecond(500*time.Millisecond)),
		roko.WithJitter(),
	).DoWithContext(ctx, func(r *roko.Retrier) error {
		var err error
		// ETags is intentionally unset in M1: the agent-managed (local) store
		// adapter doesn't require multipart e-tags. It's populated only for
		// backend-managed multipart uploads, which land in a later milestone.
		_, commitApiResp, err = c.api.CacheEntryCommit(ctx, c.registry, api.CacheEntryCommitReq{
			UploadID: createResp.UploadID,
		})
		if api.BreakOnNonRetryable(r, commitApiResp, err) {
			return err
		}
		if err != nil {
			slog.Warn("cache entry commit failed, retrying", "err", err, "retrier", r.String())
			return err
		}
		return nil
	})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to commit cache")
		return result, fmt.Errorf("failed to commit cache: %w", err)
	}

	result.CacheEntryCreated = true
	result.TotalDuration = time.Since(startTime)

	// Add final result attributes to span
	span.SetAttributes(
		attribute.Bool("cache.created", true),
		attribute.Int64("cache.duration_ms", result.TotalDuration.Milliseconds()),
	)
	span.SetStatus(codes.Ok, "cache saved successfully")

	c.callProgress(cacheID, "complete", "Cache saved successfully", 0, 0)

	return result, nil
}

// checkPathsExist validates that all paths exist on the filesystem
func checkPathsExist(paths []string) error {
	if len(paths) == 0 {
		return fmt.Errorf("no paths provided")
	}

	for _, path := range paths {
		// Handle ~ expansion
		if len(path) > 0 && path[0] == '~' {
			homeDir, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("failed to get home directory: %w", err)
			}
			path = homeDir + path[1:]
		}

		// Check if the path exists
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return fmt.Errorf("path does not exist: %s", path)
		}
	}

	return nil
}

// validateCacheStore validates the cache store configuration
func validateCacheStore(storeType, bucketURL string) error {
	if !store.IsValidStore(storeType) {
		return fmt.Errorf("unsupported cache store: %s", storeType)
	}

	switch storeType {
	case store.AgentManaged:
		if bucketURL == "" {
			return fmt.Errorf("BUILDKITE_AGENT_CACHE_STORE_URL must be set for the %s cache store", storeType)
		}
		// s3:// for S3, nsc:// for Namespace artifact storage, and file:// for
		// local testing.
		switch scheme, _, _ := strings.Cut(bucketURL, "://"); scheme {
		case "s3", "nsc", "file":
		default:
			return fmt.Errorf("unsupported cache store URL scheme %q for the %s cache store", scheme, storeType)
		}
	}

	return nil
}
