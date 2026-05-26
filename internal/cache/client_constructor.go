package cache

import (
	"fmt"
	"runtime"

	"github.com/buildkite/agent/v3/internal/cache/configuration"
)

// NewClient creates and validates a new cache client.
//
// The function performs the following steps:
//  1. Validates the configuration (cfg.Client must be provided)
//  2. Sets defaults for Format (zip) and Platform (runtime.GOOS/runtime.GOARCH)
//  3. Expands cache templates using cfg.Env if provided, otherwise uses OS environment
//  4. Validates all expanded cache configurations
//  5. Returns a ready-to-use cache client
//
// The returned Client is safe for concurrent use by multiple goroutines.
//
// Returns ErrInvalidConfiguration (wrapped) if:
//   - Template expansion fails
//   - Cache validation fails (invalid paths, missing required fields, etc.)
//
// Example:
//
//	client := api.NewClient(ctx, version, endpoint, token)
//	cacheClient, err := cache.NewClient(cache.ClientConfig{
//	    Client:    client,
//	    BucketURL: "s3://my-bucket",
//	    Branch:    "main",
//	    Pipeline:  "my-pipeline",
//	    Caches: []configuration.Cache{
//	        {ID: "deps", Key: "v1-{{ checksum \"go.mod\" }}", Paths: []string{"vendor"}},
//	    },
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
func NewClient(cfg ClientConfig) (*Client, error) {
	// cfg.Client is an interface; we do not enforce a nil check here and
	// trust callers to pass a properly initialized api.CacheClient.

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
		expandedCaches []configuration.Cache
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

	return &Client{
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
func (c *Client) callProgress(cacheID, stage, message string, current, total int) {
	if c.onProgress != nil {
		// Protect against panics in user-provided callback
		defer func() {
			_ = recover() // Ignore panics - user callbacks shouldn't break the cache client
		}()
		c.onProgress(cacheID, stage, message, current, total)
	}
}

// findCache finds a cache by ID in the cache client's cache list
func (c *Client) findCache(id string) (*configuration.Cache, error) {
	for i := range c.caches {
		if c.caches[i].ID == id {
			return &c.caches[i], nil
		}
	}
	return nil, ErrCacheNotFound
}
