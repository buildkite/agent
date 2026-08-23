package cache

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"sync"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/internal/cache/configuration"
	"github.com/dustin/go-humanize"
)

// Config holds the configuration for cache operations.
type Config struct {
	// Registry is the cache registry slug (URL path). "~" selects the cluster default.
	Registry string
	// BucketURL is the URL of the bucket (e.g., s3://bucket-name)
	BucketURL string
	// CacheConfigFile is the path to the cache configuration YAML file
	CacheConfigFile string
	// Names is a list of cache names (if empty, processes all caches)
	Names []string
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

// RunSave saves caches based on the provided configuration and logs results as
// each cache is processed.
func RunSave(ctx context.Context, l *slog.Logger, apiClient *api.Client, cfg Config) error {
	c, cacheIDs, err := newClient(l, apiClient, cfg)
	if err != nil {
		return err
	}
	if c == nil {
		l.InfoContext(ctx, "No caches defined in the cache configuration file, nothing to save")
		return nil
	}
	return saveWithClient(ctx, l, c, cacheIDs, cfg.Concurrency)
}

// RunRestore restores caches based on the provided configuration and logs results
// as each cache is processed.
func RunRestore(ctx context.Context, l *slog.Logger, apiClient *api.Client, cfg Config) error {
	c, cacheIDs, err := newClient(l, apiClient, cfg)
	if err != nil {
		return err
	}
	if c == nil {
		l.InfoContext(ctx, "No caches defined in the cache configuration file, nothing to restore")
		return nil
	}
	return restoreWithClient(ctx, l, c, cacheIDs, cfg.Concurrency)
}

// ListCaches returns all cache definitions configured on the client.
func (c *client) ListCaches() []configuration.Cache {
	return c.caches
}

// restoreWithClient performs the restore operation for the given cache IDs using the provided client.
func restoreWithClient(ctx context.Context, l *slog.Logger, c cacheOps, cacheIDs []string, concurrency int) error {
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

					l.InfoContext(wctx, "Restoring cache", "cache_id", cacheID)
					result, err := c.Restore(wctx, cacheID)
					if err != nil {
						cancel(fmt.Errorf("failed to restore cache %q: %w", cacheID, err))
						return
					}

					switch {
					case result.CacheHit, result.FallbackUsed:
						l.InfoContext(wctx, "Cache restored",
							slog.String("cache_id", cacheID), slog.String("cache_key", result.Key),
							slog.Bool("fallback_used", result.FallbackUsed),
							slog.String("archive_size", humanize.Bytes(uint64(result.Archive.Size))),
							slog.String("written_bytes", humanize.Bytes(uint64(result.Archive.WrittenBytes))),
							slog.Int64("written_entries", result.Archive.WrittenEntries),
							slog.Float64("compression_ratio", result.Archive.CompressionRatio),
							slog.String("transfer_speed", fmt.Sprintf("%.2fMB/s", result.Transfer.TransferSpeed)),
							slog.Int("part_count", result.Transfer.PartCount), slog.Int("concurrency", result.Transfer.Concurrency))
					default:
						l.InfoContext(wctx, "Cache not restored (not found)", slog.String("cache_id", cacheID), slog.String("cache_key", result.Key))
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
func saveWithClient(ctx context.Context, l *slog.Logger, c cacheOps, cacheIDs []string, concurrency int) error {
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

					l.InfoContext(wctx, "Saving cache", "cache_id", cacheID)
					result, err := c.Save(wctx, cacheID)
					if err != nil {
						cancel(fmt.Errorf("failed to save cache %q: %w", cacheID, err))
						return
					}

					switch {
					case result.CacheEntryCreated:
						l.Info("Cache created",
							slog.String("cache_id", cacheID), slog.String("cache_key", result.Key),
							slog.String("archive_size", humanize.Bytes(uint64(result.Archive.Size))),
							slog.String("written_bytes", humanize.Bytes(uint64(result.Archive.WrittenBytes))),
							slog.Int64("written_entries", result.Archive.WrittenEntries),
							slog.Float64("compression_ratio", result.Archive.CompressionRatio),
							slog.String("transfer_speed", fmt.Sprintf("%.2fMB/s", result.Transfer.TransferSpeed)),
							slog.Int("part_count", result.Transfer.PartCount), slog.Int("concurrency", result.Transfer.Concurrency))
					default:
						l.Info("Cache already exists, not saving", slog.String("cache_id", cacheID), slog.String("cache_key", result.Key))
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
