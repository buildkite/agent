package cache

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"

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
	// Ids is a list of cache IDs (if empty, processes all caches)
	Ids []string
	// APIEndpoint is the Agent API endpoint
	APIEndpoint string
	// APIToken is the access token used to authenticate
	APIToken string
	// Concurrency is the number of concurrent cache operations
	Concurrency int
}

// FileConfig represents the structure of the cache configuration YAML file
type FileConfig struct {
	// Dependencies is the list of dependency caches to restore/save
	Dependencies []cache.Cache `yaml:"dependencies"`
}

// CacheClient defines the interface for cache operations
type CacheClient interface {
	Save(ctx context.Context, cacheID string) (zstash.SaveResult, error)
	Restore(ctx context.Context, cacheID string) (zstash.RestoreResult, error)
	ListCaches() []cache.Cache
}

// Save saves caches based on the provided configuration and logs results as each cache is processed
func Save(ctx context.Context, l logger.Logger, cfg Config) error {
	cacheClient, cacheIDs, err := setupCacheClient(ctx, l, cfg)
	if err != nil {
		return err
	}

	if cacheClient == nil {
		l.Info("No caches defined in the cache configuration file, nothing to save")
		return nil
	}

	return saveWithClient(ctx, l, cacheClient, cacheIDs, cfg.Concurrency)
}

// Restore restores caches based on the provided configuration and logs results as each cache is processed
func Restore(ctx context.Context, l logger.Logger, cfg Config) error {
	cacheClient, cacheIDs, err := setupCacheClient(ctx, l, cfg)
	if err != nil {
		return err
	}

	if cacheClient == nil {
		l.Info("No caches defined in the cache configuration file, nothing to restore")
		return nil
	}

	return restoreWithClient(ctx, l, cacheClient, cacheIDs, cfg.Concurrency)
}

// loadCacheConfiguration loads cache configuration from a YAML file
func loadCacheConfiguration(cacheConfigFile string) (*FileConfig, error) {
	data, err := os.ReadFile(cacheConfigFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read cache config file: %w", err)
	}

	var config FileConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal cache config file: %w", err)
	}

	return &config, nil
}

// setupCacheClient creates a cache client and determines which cache IDs to process
func setupCacheClient(ctx context.Context, l logger.Logger, cfg Config) (*zstash.Cache, []string, error) {
	client := api.NewClient(ctx, version.Version(), cfg.APIEndpoint, cfg.APIToken)

	fileConfig, err := loadCacheConfiguration(cfg.CacheConfigFile)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load cache configuration: %w", err)
	}

	if len(fileConfig.Dependencies) == 0 {
		return nil, nil, nil
	}

	cacheClient, err := zstash.NewCache(zstash.Config{
		Client:       client,
		BucketURL:    cfg.BucketURL,
		Format:       "zip",
		Branch:       cfg.Branch,
		Pipeline:     cfg.Pipeline,
		Organization: cfg.Organization,
		Caches:       fileConfig.Dependencies,
		OnProgress: func(cacheID, stage, message string, _, _ int) {
			l.WithFields(
				logger.StringField("cache_id", cacheID),
				logger.StringField("stage", stage),
				logger.StringField("message", message),
			).Info("Cache progress")
		},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create cache client: %w", err)
	}

	// Determine which cache IDs to process
	if len(cfg.Ids) > 0 {
		// Validate that specified cache IDs exist
		validIDs := make(map[string]bool)
		for _, cache := range cacheClient.ListCaches() {
			validIDs[cache.ID] = true
		}

		var invalidIDs []string
		for _, id := range cfg.Ids {
			if !validIDs[id] {
				invalidIDs = append(invalidIDs, id)
			}
		}

		if len(invalidIDs) > 0 {
			return nil, nil, fmt.Errorf("cache IDs not found in configuration: %s", strings.Join(invalidIDs, ", "))
		}

		return cacheClient, cfg.Ids, nil
	}

	var cacheDs []string
	for _, cache := range cacheClient.ListCaches() {
		cacheDs = append(cacheDs, cache.ID)
	}

	return cacheClient, cacheDs, nil
}

// restoreWithClient performs the restore operation for the given cache IDs using the provided client
func restoreWithClient(ctx context.Context, l logger.Logger, client CacheClient, cacheIDs []string, concurrency int) error {
	if concurrency <= 0 {
		concurrency = runtime.GOMAXPROCS(0)
	}
	workerCount := min(concurrency, len(cacheIDs))

	wctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	cacheIDsCh := make(chan string)
	var wg sync.WaitGroup

	for range workerCount {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for {
				select {
				case cacheID, open := <-cacheIDsCh:
					if !open {
						return
					}

					l.Info("Restoring cache: %s", cacheID)
					result, err := client.Restore(wctx, cacheID)
					if err != nil {
						cancel(fmt.Errorf("failed to restore cache %q: %w", cacheID, err))
						return
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
							logger.IntField("part_count", result.Transfer.PartCount),
							logger.IntField("concurrency", result.Transfer.Concurrency),
						).Info("Cache restored")
					default:
						l.WithFields(
							logger.StringField("cache_id", cacheID),
							logger.StringField("cache_key", result.Key),
						).Info("Cache not restored (not found)")
					}

				case <-wctx.Done():
					return
				}
			}
		}()
	}

sendLoop:
	for _, cacheID := range cacheIDs {
		select {
		case cacheIDsCh <- cacheID:
		case <-wctx.Done():
			break sendLoop
		}
	}
	close(cacheIDsCh)

	wg.Wait()

	if err := context.Cause(wctx); err != nil {
		return err
	}

	return nil
}

// saveWithClient performs the save operation for the given cache IDs using the provided client
func saveWithClient(ctx context.Context, l logger.Logger, client CacheClient, cacheIDs []string, concurrency int) error {
	if concurrency <= 0 {
		concurrency = runtime.GOMAXPROCS(0)
	}
	workerCount := min(concurrency, len(cacheIDs))

	wctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	cacheIDsCh := make(chan string)
	var wg sync.WaitGroup

	for range workerCount {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for {
				select {
				case cacheID, open := <-cacheIDsCh:
					if !open {
						return
					}

					l.Info("Saving cache: %s", cacheID)
					result, err := client.Save(wctx, cacheID)
					if err != nil {
						cancel(fmt.Errorf("failed to save cache %q: %w", cacheID, err))
						return
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
							logger.IntField("part_count", result.Transfer.PartCount),
							logger.IntField("concurrency", result.Transfer.Concurrency),
						).Info("Cache created")
					default:
						l.WithFields(
							logger.StringField("cache_id", cacheID),
							logger.StringField("cache_key", result.Key),
						).Info("Cache already exists, not saving")
					}

				case <-wctx.Done():
					return
				}
			}
		}()
	}

sendLoop:
	for _, cacheID := range cacheIDs {
		select {
		case cacheIDsCh <- cacheID:
		case <-wctx.Done():
			break sendLoop
		}
	}
	close(cacheIDsCh)

	wg.Wait()

	if err := context.Cause(wctx); err != nil {
		return err
	}

	return nil
}
