package cache

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/agent/v3/version"
	"github.com/buildkite/zstash"
	"github.com/buildkite/zstash/api"
	"github.com/buildkite/zstash/cache"
	"github.com/dustin/go-humanize"
	"gopkg.in/yaml.v3"
)

// Config holds the configuration for cache operations
type Config struct {
	// BucketURL is the URL of the bucket (e.g., s3://bucket-name)
	BucketURL string
	// Branch is the branch associated with the cache
	Branch string
	// Pipeline is the pipeline slug for this cache
	Pipeline string
	// Organization is the organization slug for this cache
	Organization string
	// CacheConfigFile is the path to the cache configuration YAML file
	CacheConfigFile string
	// Ids is a comma-separated list of cache IDs (if empty, processes all caches)
	Ids string
	// APIEndpoint is the Agent API endpoint
	APIEndpoint string
	// APIToken is the access token used to authenticate
	APIToken string
}

// CacheClient defines the interface for cache operations
type CacheClient interface {
	Save(ctx context.Context, cacheID string) (zstash.SaveResult, error)
	Restore(ctx context.Context, cacheID string) (zstash.RestoreResult, error)
	ListCaches() []cache.Cache
}

// Save saves caches based on the provided configuration and logs results as each cache is processed
func Save(ctx context.Context, l logger.Logger, cfg Config) error {
	cacheClient, cacheIDs, err := setupCacheClient(ctx, cfg)
	if err != nil {
		return err
	}

	if cacheClient == nil {
		l.Info("No caches defined in the cache configuration file, nothing to save")
		return nil
	}

	return saveWithClient(ctx, l, cacheClient, cacheIDs)
}

// Restore restores caches based on the provided configuration and logs results as each cache is processed
func Restore(ctx context.Context, l logger.Logger, cfg Config) error {
	cacheClient, cacheIDs, err := setupCacheClient(ctx, cfg)
	if err != nil {
		return err
	}

	if cacheClient == nil {
		l.Info("No caches defined in the cache configuration file, nothing to restore")
		return nil
	}

	return restoreWithClient(ctx, l, cacheClient, cacheIDs)
}

// loadCacheConfiguration loads cache configuration from a YAML file
func loadCacheConfiguration(cacheConfigFile string) ([]cache.Cache, error) {
	data, err := os.ReadFile(cacheConfigFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read cache config file: %w", err)
	}

	var caches []cache.Cache
	if err := yaml.Unmarshal(data, &caches); err != nil {
		return nil, fmt.Errorf("failed to unmarshal cache config file: %w", err)
	}

	return caches, nil
}

// setupCacheClient creates a cache client and determines which cache IDs to process
func setupCacheClient(ctx context.Context, cfg Config) (*zstash.Cache, []string, error) {
	client := api.NewClient(ctx, version.Version(), cfg.APIEndpoint, cfg.APIToken)

	caches, err := loadCacheConfiguration(cfg.CacheConfigFile)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load cache configuration: %w", err)
	}

	if len(caches) == 0 {
		return nil, nil, nil
	}

	cacheClient, err := zstash.NewCache(zstash.Config{
		Client:       client,
		BucketURL:    cfg.BucketURL,
		Format:       "zip",
		Branch:       cfg.Branch,
		Pipeline:     cfg.Pipeline,
		Organization: cfg.Organization,
		Caches:       caches,
		OnProgress: func(stage, message string, current, total int) {
			l.WithFields(
				logger.StringField("stage", stage),
				logger.StringField("message", message),
				logger.IntField("current", current),
				logger.IntField("total", total),
			).Info("Cache progress")
		},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create cache client: %w", err)
	}

	// Determine which cache IDs to process
	var cacheIDs []string
	if cfg.Ids != "" {
		cacheIDs = strings.Split(cfg.Ids, ",")
	} else {
		// Process all caches configured in the client
		for _, cache := range cacheClient.ListCaches() {
			cacheIDs = append(cacheIDs, cache.ID)
		}
	}

	return cacheClient, cacheIDs, nil
}

// restoreWithClient performs the restore operation for the given cache IDs using the provided client
func restoreWithClient(ctx context.Context, l logger.Logger, client CacheClient, cacheIDs []string) error {
	for _, cacheID := range cacheIDs {
		l.Info("Restoring cache: %s", cacheID)
		result, err := client.Restore(ctx, cacheID)
		if err != nil {
			return fmt.Errorf("failed to restore cache %q: %w", cacheID, err)
		}

		switch {
		case result.CacheHit, result.FallbackUsed:
			l.WithFields(
				logger.StringField("cache_id", cacheID),
				logger.StringField("cache_key", result.Key),
				logger.StringField("fallback_used", fmt.Sprintf("%t", result.FallbackUsed)),
				logger.StringField("archive_size", humanize.Bytes(uint64(result.Archive.Size))),
				logger.StringField("written_bytes", humanize.Bytes(uint64(result.Archive.WrittenBytes))),
				logger.StringField("written_entries", fmt.Sprintf("%d", result.Archive.WrittenEntries)),
				logger.StringField("compression_ratio", fmt.Sprintf("%.2f", result.Archive.CompressionRatio)),
				logger.StringField("transfer_speed", fmt.Sprintf("%.2fMB/s", result.Transfer.TransferSpeed)),
			).Info("Cache restored")
		default:
			l.WithFields(
				logger.StringField("cache_id", cacheID),
				logger.StringField("cache_key", result.Key),
			).Info("Cache not restored (not found)")
		}
	}

	return nil
}

// saveWithClient performs the save operation for the given cache IDs using the provided client
func saveWithClient(ctx context.Context, l logger.Logger, client CacheClient, cacheIDs []string) error {
	for _, cacheID := range cacheIDs {
		l.Info("Saving cache: %s", cacheID)
		result, err := client.Save(ctx, cacheID)
		if err != nil {
			return fmt.Errorf("failed to save cache %q: %w", cacheID, err)
		}

		switch {
		case result.CacheCreated:
			l.WithFields(
				logger.StringField("cache_id", cacheID),
				logger.StringField("cache_key", result.Key),
				logger.StringField("archive_size", humanize.Bytes(uint64(result.Archive.Size))),
				logger.StringField("written_bytes", humanize.Bytes(uint64(result.Archive.WrittenBytes))),
				logger.StringField("written_entries", fmt.Sprintf("%d", result.Archive.WrittenEntries)),
				logger.StringField("compression_ratio", fmt.Sprintf("%.2f", result.Archive.CompressionRatio)),
				logger.StringField("transfer_speed", fmt.Sprintf("%.2fMB/s", result.Transfer.TransferSpeed)),
			).Info("Cache created")
		default:
			l.WithFields(
				logger.StringField("cache_id", cacheID),
				logger.StringField("cache_key", result.Key),
			).Info("Cache already exists, not saving")
		}
	}

	return nil
}
