// Package cache saves and restores cache archives via the Buildkite cache API.
//
// The CLI entry points are Save and Restore in cache.go. Internally they build
// a client (a configured handle bundling the API client, storage bucket and
// the expanded cache definitions) and dispatch Save/Restore per cache ID.
package cache

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/internal/cache/configuration"
	"github.com/buildkite/agent/v3/logger"
)

// cacheAPI is the subset of *api.Client that the cache package depends on.
// It exists so integration tests can substitute a fake.
//
// TODO: drop this interface and let `api` field hold *api.Client directly.
// The integration test's mockAPIClient should be replaced with an httptest
// server returning canned responses — this repo prefers real HTTP servers
// over hand-rolled mocks.
type cacheAPI interface {
	CacheRegistry(ctx context.Context, registry string) (api.CacheRegistryResp, error)
	CachePeekExists(ctx context.Context, registry string, req api.CachePeekReq) (api.CachePeekResp, bool, error)
	CacheCreate(ctx context.Context, registry string, req api.CacheCreateReq) (api.CacheCreateResp, error)
	CacheCommit(ctx context.Context, registry string, req api.CacheCommitReq) (api.CacheCommitResp, error)
	CacheRetrieve(ctx context.Context, registry string, req api.CacheRetrieveReq) (api.CacheRetrieveResp, bool, error)
}

// Sentinel errors for common scenarios.
var (
	// ErrCacheNotFound is returned when a requested cache ID doesn't exist in
	// the loaded configuration.
	ErrCacheNotFound = errors.New("cache not found")

	// ErrInvalidConfiguration is returned when configuration validation fails
	// while building a client.
	ErrInvalidConfiguration = errors.New("invalid configuration")
)

// client is a configured handle for cache Save and Restore operations.
//
// It is not a network connection; it just bundles the API client, storage
// bucket and the expanded, validated cache definitions used by every call.
// Safe for concurrent use; honours context cancellation.
type client struct {
	api          cacheAPI
	bucketURL    string
	format       string
	branch       string
	pipeline     string
	organization string
	platform     string
	registry     string
	caches       []configuration.Cache
	onProgress   ProgressCallback
}

// newClient builds a client from apiClient and cfg: loads and expands the
// cache configuration file, validates every cache definition, and returns the
// client together with the cache IDs to operate on (filtered by cfg.Ids if
// non-empty).
//
// Returns (nil, nil, nil) when the configuration file has no caches.
// Returns ErrInvalidConfiguration (wrapped) on expansion or validation failure.
func newClient(l logger.Logger, apiClient cacheAPI, cfg Config) (*client, []string, error) {
	caches, err := configuration.LoadFile(cfg.CacheConfigFile)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load cache configuration: %w", err)
	}
	if len(caches) == 0 {
		return nil, nil, nil
	}

	expanded, err := configuration.ExpandCacheConfiguration(caches)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: failed to expand cache configuration: %w", ErrInvalidConfiguration, err)
	}
	for _, c := range expanded {
		if err := c.Validate(); err != nil {
			return nil, nil, fmt.Errorf("%w: cache validation failed for ID %s: %w", ErrInvalidConfiguration, c.ID, err)
		}
	}

	c := &client{
		api:          apiClient,
		bucketURL:    cfg.BucketURL,
		format:       "zip",
		branch:       cfg.Branch,
		pipeline:     cfg.Pipeline,
		organization: cfg.Organization,
		platform:     fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
		registry:     "~",
		caches:       expanded,
		onProgress: func(cacheID, stage, message string, _, _ int) {
			l.WithFields(
				logger.StringField("cache_id", cacheID),
				logger.StringField("stage", stage),
				logger.StringField("message", message),
			).Infof("Cache progress")
		},
	}

	ids, err := c.resolveCacheIDs(cfg.Ids)
	if err != nil {
		return nil, nil, err
	}
	return c, ids, nil
}

// resolveCacheIDs returns requested if non-empty (after validating every ID
// exists), otherwise returns every cache ID configured on the client.
func (c *client) resolveCacheIDs(requested []string) ([]string, error) {
	if len(requested) == 0 {
		ids := make([]string, 0, len(c.caches))
		for _, cc := range c.caches {
			ids = append(ids, cc.ID)
		}
		return ids, nil
	}

	known := make(map[string]bool, len(c.caches))
	for _, cc := range c.caches {
		known[cc.ID] = true
	}
	var invalid []string
	for _, id := range requested {
		if !known[id] {
			invalid = append(invalid, id)
		}
	}
	if len(invalid) > 0 {
		return nil, fmt.Errorf("cache IDs not found in configuration: %s", strings.Join(invalid, ", "))
	}
	return requested, nil
}

// callProgress safely invokes the progress callback if set, recovering from
// panics in caller-supplied callbacks.
func (c *client) callProgress(cacheID, stage, message string, current, total int) {
	if c.onProgress == nil {
		return
	}
	defer func() {
		_ = recover()
	}()
	c.onProgress(cacheID, stage, message, current, total)
}

// findCache returns the cache definition with the given ID, or ErrCacheNotFound.
func (c *client) findCache(id string) (*configuration.Cache, error) {
	for i := range c.caches {
		if c.caches[i].ID == id {
			return &c.caches[i], nil
		}
	}
	return nil, ErrCacheNotFound
}

// ProgressCallback is invoked during Save and Restore to report progress.
//
// Implementations must be thread-safe; the callback may be called from
// multiple goroutines. See save.go and restore.go for the stage values.
type ProgressCallback func(cacheID, stage, message string, current, total int)

// SaveResult contains detailed information about a cache save operation.
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

	// Archive contains information about the archive that was built.
	Archive ArchiveMetrics

	// Transfer contains information about the upload (if performed).
	// Nil if CacheCreated is false (cache already existed).
	Transfer *TransferMetrics

	// TotalDuration is the end-to-end duration of the save operation.
	TotalDuration time.Duration
}

// RestoreResult contains detailed information about a cache restore operation.
type RestoreResult struct {
	// CacheHit indicates whether the exact cache key was found.
	CacheHit bool

	// CacheRestored indicates whether any cache was restored (including fallbacks).
	CacheRestored bool

	// Key is the actual cache key that was restored.
	Key string

	// FallbackUsed indicates whether a fallback key was used.
	FallbackUsed bool

	// Archive contains information about the archive that was extracted.
	Archive ArchiveMetrics

	// Transfer contains information about the download operation.
	Transfer TransferMetrics

	// ExpiresAt indicates when this cache entry will expire.
	ExpiresAt time.Time

	// TotalDuration is the end-to-end duration of the restore operation.
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
	CompressionRatio float64

	// Sha256Sum is the SHA-256 hash of the archive file (save only).
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
	RequestID string

	// PartCount is the number of parts used in multipart transfer (0 if not multipart).
	PartCount int

	// Concurrency is the number of concurrent uploads/downloads used.
	Concurrency int
}
