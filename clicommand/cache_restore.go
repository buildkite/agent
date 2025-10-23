package clicommand

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/agent/v3/version"
	"github.com/buildkite/zstash"
	"github.com/buildkite/zstash/api"
	"github.com/dustin/go-humanize"
	"github.com/urfave/cli"
)

const cacheRestoreHelpDescription = `Usage:

    buildkite-agent cache restore [options]

Description:

Restores files from the cache for the current build based on the cache configuration
defined in your cache config file (defaults to .buildkite/cache.yml).

The cache configuration file defines which files or directories should be restored
and their associated cache keys. Caches are scoped by organization, pipeline, and
branch. If an exact cache match is not found, the command will attempt to use
fallback keys if defined in your cache configuration.

Example:

    $ buildkite-agent cache restore

This will restore all caches defined in .buildkite/cache.yml. You can also restore
specific caches by providing their IDs:

    $ buildkite-agent cache restore --ids "node"

The cache will be retrieved from the bucket specified by --bucket-url or your
cache configuration.

Configuration File Format:

The cache configuration file should be in YAML format:

    - id: node
      key: '{{ id }}-{{ agent.os }}-{{ agent.arch }}-{{ checksum "package-lock.json" }}'
	  fallback_keys:
		- '{{ id }}-{{ agent.os }}-{{ agent.arch }}-'
      paths:
        - node_modules

Cache Restoration Results:

The command will report one of three outcomes for each cache:
  - Cache hit: Exact key match found and restored
  - Fallback used: No exact match, but a fallback key was found and restored
  - Cache miss: No matching cache found

The command automatically uses the following environment variables when available:
  - BUILDKITE_BRANCH (for branch scoping)
  - BUILDKITE_PIPELINE_SLUG (for pipeline scoping)
  - BUILDKITE_ORGANIZATION_SLUG (for organization scoping)`

type CacheRestoreConfig struct {
	GlobalConfig
	APIConfig
	CacheConfig
}

var CacheRestoreCommand = cli.Command{
	Name:        "restore",
	Usage:       "Restores files from the cache",
	Description: cacheRestoreHelpDescription,
	Flags:       slices.Concat(globalFlags(), apiFlags(), cacheFlags()),
	Action: func(c *cli.Context) error {
		ctx := context.Background()
		ctx, cfg, l, _, done := setupLoggerAndConfig[CacheRestoreConfig](ctx, c)
		defer done()

		l.Info("Cache restore command executed")

		apiCfg := loadAPIClientConfig(cfg, "AgentAccessToken")

		// we are using the zstash api package here which has a different client constructor but uses the same values
		client := api.NewClient(ctx, version.Version(), apiCfg.Endpoint, apiCfg.Token)

		caches, err := loadCacheConfiguration(cfg.CacheConfigFile)
		if err != nil {
			return fmt.Errorf("failed to load cache configuration: %w", err)
		}

		if len(caches) == 0 {
			l.Info("No caches defined in the cache configuration file, nothing to save")
			return nil
		}

		cacheClient, err := zstash.NewCache(zstash.Config{
			Client:       client,
			BucketURL:    cfg.BucketURL,
			Format:       "zip",
			Branch:       cfg.Branch,
			Pipeline:     cfg.Pipeline,
			Organization: cfg.Organization,
			Caches:       caches,
		})
		if err != nil {
			return fmt.Errorf("failed to create cache client: %w", err)
		}

		// split the ids by comma
		cacheIDs := strings.Split(cfg.Ids, ",")

		if len(cacheIDs) == 0 {
			// Save all caches configured in the client
			caches := cacheClient.ListCaches()
			for _, cache := range caches {
				cacheIDs = append(cacheIDs, cache.ID)
			}
		}

		for _, cacheID := range cacheIDs {
			l.Info("Restoring cache: %s", cacheID)
			result, err := cacheClient.Restore(ctx, cacheID)
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
				// Cache was not found
				l.WithFields(
					logger.StringField("cache_id", cacheID),
					logger.StringField("cache_key", result.Key),
				).Info("Cache not restored (not found)")
			}
		}

		return nil
	},
}
