package clicommand

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/buildkite/agent/v3/version"
	"github.com/buildkite/zstash"
	"github.com/buildkite/zstash/api"
	"github.com/dustin/go-humanize"
	"github.com/urfave/cli"
)

const cacheSaveHelpDescription = `Usage:

    buildkite-agent cache save [options]

Description:

Saves files to the cache for the current build based on the cache configuration
defined in your cache config file (defaults to .buildkite/cache.yml).

The cache configuration file defines which files or directories should be cached
and their associated cache keys. Caches are scoped by organization, pipeline, and
branch.

Example:

    $ buildkite-agent cache save

This will save all caches defined in .buildkite/cache.yml. You can also save
specific caches by providing their IDs:

    $ buildkite-agent cache save --ids "node"

The cache will be stored in the bucket specified by --bucket-url or your
cache configuration. If a cache with the same key already exists, it will
not be overwritten.

Configuration File Format:

The cache configuration file should be in YAML format:

    - id: node
      key: '{{ id }}-{{ agent.os }}-{{ agent.arch }}-{{ checksum "package-lock.json" }}'
	  fallback_keys:
		- '{{ id }}-{{ agent.os }}-{{ agent.arch }}-'
      paths:
        - node_modules

The command automatically uses the following environment variables when available:
  - BUILDKITE_BRANCH (for branch scoping)
  - BUILDKITE_PIPELINE_SLUG (for pipeline scoping)
  - BUILDKITE_ORGANIZATION_SLUG (for organization scoping)`

type CacheSaveConfig struct {
	GlobalConfig
	APIConfig
	CacheConfig
}

var CacheSaveCommand = cli.Command{
	Name:        "save",
	Usage:       "Saves files to the cache",
	Description: cacheSaveHelpDescription,
	Flags:       slices.Concat(globalFlags(), apiFlags(), cacheFlags()),
	Action: func(c *cli.Context) error {
		ctx := context.Background()
		ctx, cfg, l, _, done := setupLoggerAndConfig[CacheSaveConfig](ctx, c)
		defer done()

		l.Info("Cache save command executed")

		apiCfg := loadAPIClientConfig(cfg, "AgentAccessToken")

		// we are using the zstash api package here which has a different client constructor but uses the same values
		client := api.NewClient(ctx, version.Version(), apiCfg.Endpoint, apiCfg.Token)

		caches, err := loadCacheConfiguration(cfg.CacheConfigFile)
		if err != nil {
			return fmt.Errorf("failed to load cache configuration: %w", err)
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
			result, err := cacheClient.Save(ctx, cacheID)
			if err != nil {
				return fmt.Errorf("failed to save cache %q: %w", cacheID, err)
			}

			switch {
			case result.CacheCreated:
				l.Info("Cache created", map[string]interface{}{
					"cache_id":          cacheID,
					"cache_key":         result.Key,
					"archive_size":      humanize.Bytes(uint64(result.Archive.Size)),
					"written_bytes":     humanize.Bytes(uint64(result.Archive.WrittenBytes)),
					"written_entries":   fmt.Sprintf("%d", result.Archive.WrittenEntries),
					"compression_ratio": fmt.Sprintf("%.2f", result.Archive.CompressionRatio),
					"transfer_speed":    fmt.Sprintf("%.2fMB/s", result.Transfer.TransferSpeed),
				})
			default:
				l.Info("Cache already exists, not saving", map[string]interface{}{
					"cache_id":  cacheID,
					"cache_key": result.Key,
				})
			}
		}

		return nil
	},
}
