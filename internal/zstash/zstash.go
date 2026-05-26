// Package zstash provides a library for saving and restoring cache archives
// to/from cloud storage with the Buildkite cache API.
//
// The main entry point is NewCache, which creates a Cache client for managing
// cache operations. The Cache client is safe for concurrent use by multiple
// goroutines.
//
// Basic usage:
//
//	client := api.NewClient(ctx, version, endpoint, token)
//	cacheClient, err := zstash.NewCache(zstash.Config{
//	    Client:       client,
//	    BucketURL:    "s3://my-bucket",
//	    Branch:       "main",
//	    Pipeline:     "my-pipeline",
//	    Organization: "my-org",
//	    Caches: []cache.Cache{
//	        {ID: "node_modules", Key: "v1-{{ checksum \"package-lock.json\" }}", Paths: []string{"node_modules"}},
//	    },
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Save a cache
//	result, err := cacheClient.Save(ctx, "node_modules")
//
//	// Restore a cache
//	result, err := cacheClient.Restore(ctx, "node_modules")
package zstash

import (
	"errors"
	"time"

	"github.com/buildkite/agent/v3/internal/zstash/api"
	"github.com/buildkite/agent/v3/internal/zstash/cache"
)

// Sentinel errors for common scenarios
var (
	// ErrCacheNotFound is returned when a requested cache ID doesn't exist
	// in the cache client's configuration.
	ErrCacheNotFound = errors.New("cache not found")

	// ErrInvalidConfiguration is returned when configuration validation fails
	// during cache client creation.
	ErrInvalidConfiguration = errors.New("invalid configuration")
)

// Cache provides cache save and restore operations with the Buildkite cache API.
//
// A Cache client is created once with configuration and can be used for multiple
// operations. The client is safe for concurrent use by multiple goroutines.
//
// All cache operations respect context cancellation and will clean up resources
// when the context is cancelled.
type Cache struct {
	client       api.CacheClient
	bucketURL    string
	format       string
	branch       string
	pipeline     string
	organization string
	platform     string
	registry     string
	caches       []cache.Cache
	onProgress   ProgressCallback
}

// Config holds all configuration for creating a Cache client.
//
// The only required field is Client. All other fields have sensible defaults or
// are optional depending on your use case.
type Config struct {
	// Client is the Buildkite API client (required).
	// Create with api.NewClient(ctx, version, endpoint, token).
	Client api.CacheClient

	// BucketURL is the storage backend URL (required for most store types).
	// Examples: "s3://bucket-name", "gs://bucket-name", "file:///path/to/dir"
	BucketURL string

	// Format is the archive format. Defaults to "zip" if not specified.
	Format string

	// Branch is the git branch name, used for cache scoping in the Buildkite API.
	Branch string

	// Pipeline is the pipeline slug, used for cache scoping in the Buildkite API.
	Pipeline string

	// Organization is the organization slug, used for cache scoping in the Buildkite API.
	Organization string

	// Platform is the OS/arch string (e.g., "linux/amd64", "darwin/arm64").
	// If empty, defaults to runtime.GOOS/runtime.GOARCH.
	Platform string

	// Registry is the default cache registry to use for all cache operations.
	// If empty, defaults to "~" (the default registry).
	// Individual cache configurations can override this by setting their own Registry field.
	Registry string

	// Env is an optional environment variable map used for cache template expansion.
	// If nil, OS environment variables are used instead via os.Getenv.
	// Cache keys and paths can use templates like "{{ env \"NODE_VERSION\" }}".
	Env map[string]string

	// Caches is the list of cache configurations to manage.
	// Cache keys and paths will be expanded using template variables.
	Caches []cache.Cache

	// OnProgress is an optional callback for progress updates during operations.
	// If nil, no progress callbacks are made. The callback must be thread-safe
	// as it may be called from multiple goroutines.
	OnProgress ProgressCallback
}

// ProgressCallback is called during long-running operations to report progress.
//
// The callback is invoked at various stages during Save and Restore operations
// to provide visibility into the operation's progress. Implementations must be
// thread-safe as the callback may be called from multiple goroutines.
//
// Parameters:
//   - cacheID: The ID of the cache being operated on.
//   - stage: The current operation stage. See below for possible values.
//   - message: A human-readable description of the current action.
//   - current: Current progress value (bytes transferred, files processed, etc.).
//   - total: Total expected value (0 if unknown).
//
// Save operation stages:
//   - "validating": Validating cache configuration
//   - "checking_exists": Checking if cache already exists
//   - "fetching_registry": Looking up cache registry
//   - "building_archive": Building archive (current=files processed, total=total files)
//   - "creating_entry": Creating cache entry in API
//   - "uploading": Uploading cache (current=bytes sent, total=total bytes)
//   - "committing": Committing cache entry
//   - "complete": Operation finished successfully
//
// Restore operation stages:
//   - "validating": Validating cache configuration
//   - "checking_exists": Checking if cache exists
//   - "downloading": Downloading cache (current=bytes received, total=total bytes)
//   - "extracting": Extracting files (current=files extracted, total=total files)
//   - "complete": Operation finished successfully
type ProgressCallback func(cacheID, stage, message string, current, total int)

// NewCache creates and validates a new cache client.
// Implementation is in service.go

// Save and Restore methods are implemented in save.go and restore.go

// ListCaches returns all cache configurations managed by this cache client.
//
// The returned caches have already been expanded (templates resolved) and
// validated during NewCache construction.
func (c *Cache) ListCaches() []cache.Cache {
	return c.caches
}

// GetCache returns a specific cache configuration by ID.
//
// Returns ErrCacheNotFound if the cache ID is not found in the client's
// configuration.
func (c *Cache) GetCache(id string) (cache.Cache, error) {
	for _, cacheItem := range c.caches {
		if cacheItem.ID == id {
			return cacheItem, nil
		}
	}
	return cache.Cache{}, ErrCacheNotFound
}

// SaveResult contains detailed information about a cache save operation.
//
// Check CacheCreated to see if a new cache was uploaded or if the cache
// already existed.
type SaveResult struct {
	// CacheCreated indicates whether a new cache entry was created.
	// false means the cache already existed and no upload occurred.
	// When false, Transfer will be nil since no upload was performed.
	CacheCreated bool

	// Key is the actual cache key that was used (after template expansion).
	Key string

	// UploadID is the unique identifier for this upload (if created).
	// Empty if CacheCreated is false.
	UploadID string

	// Archive contains information about the archive that was built,
	// including size, compression ratio, and file counts.
	Archive ArchiveMetrics

	// Transfer contains information about the upload (if performed).
	// Nil if CacheCreated is false (cache already existed).
	Transfer *TransferMetrics

	// TotalDuration is the end-to-end duration of the save operation,
	// from validation through commit (if created) or early exit (if exists).
	TotalDuration time.Duration
}

// RestoreResult contains detailed information about a cache restore operation.
//
// Check CacheRestored to see if a cache was found.
// Use CacheHit and FallbackUsed to determine if the exact key matched.
type RestoreResult struct {
	// CacheHit indicates whether the exact cache key was found.
	// false means either no cache found, or a fallback key was used.
	// When false, check CacheRestored to distinguish between cache miss
	// and fallback hit.
	CacheHit bool

	// CacheRestored indicates whether any cache was restored (including fallbacks).
	// false means complete cache miss (no matching key or fallback keys).
	CacheRestored bool

	// Key is the actual cache key that was restored.
	// May differ from the requested key if FallbackUsed is true.
	Key string

	// FallbackUsed indicates whether a fallback key was used.
	// true means the exact key wasn't found, but a fallback key matched.
	FallbackUsed bool

	// Archive contains information about the archive that was extracted,
	// including size, compression ratio, and file counts.
	Archive ArchiveMetrics

	// Transfer contains information about the download operation,
	// including bytes transferred and transfer speed.
	Transfer TransferMetrics

	// ExpiresAt indicates when this cache entry will expire.
	ExpiresAt time.Time

	// TotalDuration is the end-to-end duration of the restore operation,
	// from validation through extraction.
	TotalDuration time.Duration
}

// ArchiveMetrics contains metrics about archive build and extraction operations.
type ArchiveMetrics struct {
	// Size is the total size of the archive file in bytes (compressed).
	Size int64

	// WrittenBytes is the uncompressed size of all files in bytes.
	WrittenBytes int64

	// WrittenEntries is the number of files/directories in the archive.
	WrittenEntries int64

	// CompressionRatio is WrittenBytes / Size.
	// Higher values indicate better compression (e.g., 3.0 means 3:1 compression).
	CompressionRatio float64

	// Sha256Sum is the SHA-256 hash of the archive file.
	// Only populated for save operations, empty for restore.
	Sha256Sum string

	// Duration is how long the archive build or extraction took.
	Duration time.Duration

	// Paths are the filesystem paths that were archived or extracted.
	Paths []string
}

// TransferMetrics contains metrics about upload and download operations.
type TransferMetrics struct {
	// BytesTransferred is the number of bytes uploaded or downloaded.
	BytesTransferred int64

	// TransferSpeed is the transfer rate in MB/s.
	TransferSpeed float64

	// Duration is how long the transfer took.
	Duration time.Duration

	// RequestID is the provider-specific request identifier for debugging.
	// Format depends on the storage backend (e.g., S3 request ID).
	RequestID string

	// PartCount is the number of parts used in multipart transfer (0 if not multipart).
	PartCount int

	// Concurrency is the number of concurrent uploads/downloads used.
	Concurrency int
}
