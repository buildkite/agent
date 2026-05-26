package cache

import (
	"context"
	"fmt"
	"runtime"
	"sync"

	"github.com/buildkite/agent/v3/internal/cache/configuration"
	"github.com/buildkite/agent/v3/logger"
	"github.com/dustin/go-humanize"
)

// Config holds the configuration for cache operations.
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

// cacheOps is the subset of *client used by saveWithClient and restoreWithClient.
// It exists so the dispatch loops can be tested with a fake.
type cacheOps interface {
	Save(ctx context.Context, cacheID string) (SaveResult, error)
	Restore(ctx context.Context, cacheID string) (RestoreResult, error)
	ListCaches() []configuration.Cache
}

// Save saves caches based on the provided configuration and logs results as
// each cache is processed.
func Save(ctx context.Context, l logger.Logger, cfg Config) error {
	c, cacheIDs, err := newClient(ctx, l, cfg)
	if err != nil {
		return err
	}
	if c == nil {
		l.Infof("No caches defined in the cache configuration file, nothing to save")
		return nil
	}
	return saveWithClient(ctx, l, c, cacheIDs, cfg.Concurrency)
}

// Restore restores caches based on the provided configuration and logs results
// as each cache is processed.
func Restore(ctx context.Context, l logger.Logger, cfg Config) error {
	c, cacheIDs, err := newClient(ctx, l, cfg)
	if err != nil {
		return err
	}
	if c == nil {
		l.Infof("No caches defined in the cache configuration file, nothing to restore")
		return nil
	}
	return restoreWithClient(ctx, l, c, cacheIDs, cfg.Concurrency)
}

// ListCaches returns all cache definitions configured on the client.
func (c *client) ListCaches() []configuration.Cache {
	return c.caches
}

// restoreWithClient performs the restore operation for the given cache IDs using the provided client.
func restoreWithClient(ctx context.Context, l logger.Logger, c cacheOps, cacheIDs []string, concurrency int) error {
	if concurrency <= 0 {
		concurrency = runtime.GOMAXPROCS(0)
	}
	workerCount := min(concurrency, len(cacheIDs))

	wctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	cacheIDsCh := make(chan string)
	var wg sync.WaitGroup

	for range workerCount {
		wg.Go(func() {
			for {
				select {
				case cacheID, open := <-cacheIDsCh:
					if !open {
						return
					}

					l.Infof("Restoring cache: %s", cacheID)
					result, err := c.Restore(wctx, cacheID)
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
						).Infof("Cache restored")
					default:
						l.WithFields(
							logger.StringField("cache_id", cacheID),
							logger.StringField("cache_key", result.Key),
						).Infof("Cache not restored (not found)")
					}

				case <-wctx.Done():
					return
				}
			}
		})
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

// saveWithClient performs the save operation for the given cache IDs using the provided client.
func saveWithClient(ctx context.Context, l logger.Logger, c cacheOps, cacheIDs []string, concurrency int) error {
	if concurrency <= 0 {
		concurrency = runtime.GOMAXPROCS(0)
	}
	workerCount := min(concurrency, len(cacheIDs))

	wctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	cacheIDsCh := make(chan string)
	var wg sync.WaitGroup

	for range workerCount {
		wg.Go(func() {
			for {
				select {
				case cacheID, open := <-cacheIDsCh:
					if !open {
						return
					}

					l.Infof("Saving cache: %s", cacheID)
					result, err := c.Save(wctx, cacheID)
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
						).Infof("Cache created")
					default:
						l.WithFields(
							logger.StringField("cache_id", cacheID),
							logger.StringField("cache_key", result.Key),
						).Infof("Cache already exists, not saving")
					}

				case <-wctx.Done():
					return
				}
			}
		})
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
