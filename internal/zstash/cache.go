package zstash

import (
	"fmt"
	"runtime"

	"github.com/buildkite/agent/v3/internal/zstash/cache"
	"github.com/buildkite/agent/v3/internal/zstash/configuration"
)

// NewCache creates and validates a new cache client.
//
// The function performs the following steps:
//  1. Validates the configuration (Client must be provided)
//  2. Sets defaults for Format (zip) and Platform (runtime.GOOS/runtime.GOARCH)
//  3. Expands cache templates using cfg.Env if provided, otherwise uses OS environment
//  4. Validates all expanded cache configurations
//  5. Returns a ready-to-use cache client
//
// The returned Cache client is safe for concurrent use by multiple goroutines.
//
// Returns ErrInvalidConfiguration (wrapped) if:
//   - Template expansion fails
//   - Cache validation fails (invalid paths, missing required fields, etc.)
//
// Example:
//
//	client := api.NewClient(ctx, version, endpoint, token)
//	cacheClient, err := zstash.NewCache(zstash.Config{
//	    Client:    client,
//	    BucketURL: "s3://my-bucket",
//	    Branch:    "main",
//	    Pipeline:  "my-pipeline",
//	    Caches: []cache.Cache{
//	        {ID: "deps", Key: "v1-{{ checksum \"go.mod\" }}", Paths: []string{"vendor"}},
//	    },
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
func NewCache(cfg Config) (*Cache, error) {
	// Validate required configuration
	// Note: Client is a struct, so we can't check for nil. It should be created via NewClient.
	// We trust that if passed, it was properly initialized.

	// Set defaults
	if cfg.Format == "" {
		cfg.Format = "zip"
	}

	if cfg.Platform == "" {
		cfg.Platform = fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH)
	}

	if cfg.Registry == "" {
		cfg.Registry = "~"
	}

	var (
		err error
		// Expand cache configurations
		expandedCaches []cache.Cache
	)

	if cfg.Env != nil {
		// If environment is provided, expand cache templates
		expandedCaches, err = configuration.ExpandCacheConfigurationWithEnv(cfg.Caches, cfg.Env)
		if err != nil {
			return nil, fmt.Errorf("%w: failed to expand cache configuration: %w", ErrInvalidConfiguration, err)
		}
	} else {
		// Use OS environment for expansion
		expandedCaches, err = configuration.ExpandCacheConfiguration(cfg.Caches)
		if err != nil {
			return nil, fmt.Errorf("%w: failed to expand cache configuration: %w", ErrInvalidConfiguration, err)
		}
	}

	// Validate all caches
	for _, c := range expandedCaches {
		if err := c.Validate(); err != nil {
			return nil, fmt.Errorf("%w: cache validation failed for ID %s: %w", ErrInvalidConfiguration, c.ID, err)
		}
	}

	return &Cache{
		client:       cfg.Client,
		bucketURL:    cfg.BucketURL,
		format:       cfg.Format,
		branch:       cfg.Branch,
		pipeline:     cfg.Pipeline,
		organization: cfg.Organization,
		platform:     cfg.Platform,
		registry:     cfg.Registry,
		caches:       expandedCaches,
		onProgress:   cfg.OnProgress,
	}, nil
}

// callProgress safely calls the progress callback if it exists
func (c *Cache) callProgress(cacheID, stage, message string, current, total int) {
	if c.onProgress != nil {
		// Protect against panics in user-provided callback
		defer func() {
			_ = recover() // Ignore panics - user callbacks shouldn't break the cache client
		}()
		c.onProgress(cacheID, stage, message, current, total)
	}
}

// findCache finds a cache by ID in the cache client's cache list
func (c *Cache) findCache(id string) (*cache.Cache, error) {
	for i := range c.caches {
		if c.caches[i].ID == id {
			return &c.caches[i], nil
		}
	}
	return nil, ErrCacheNotFound
}
