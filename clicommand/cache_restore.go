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

	Ids             string `cli:"ids"`
	BucketURL       string `cli:"bucket-url"`
	Branch          string `cli:"branch" validate:"required"`
	Pipeline        string `cli:"pipeline" validate:"required"`
	Organization    string `cli:"organization" validate:"required"`
	CacheConfigFile string `cli:"cache-config-file"`
}

var CacheRestoreCommand = cli.Command{
	Name:        "restore",
	Usage:       "Restores files from the cache",
	Description: cacheRestoreHelpDescription,
	Flags: slices.Concat(globalFlags(), apiFlags(), []cli.Flag{
		cli.StringFlag{
			Name:   "ids",
			Value:  "",
			Usage:  "Comma-separated list of cache IDs to restore (if empty, restores all caches)",
			EnvVar: "BUILDKITE_CACHE_IDS",
		},
		cli.StringFlag{
			Name:   "bucket-url",
			Value:  "",
			Usage:  "The URL of the bucket to retrieve caches from (e.g., s3://bucket-name)",
			EnvVar: "BUILDKITE_CACHE_BUCKET_URL",
		},
		cli.StringFlag{
			Name:   "branch",
			Value:  "",
			Usage:  "Which branch should the cache be associated with",
			EnvVar: "BUILDKITE_BRANCH",
		},
		cli.StringFlag{
			Name:   "pipeline",
			Value:  "",
			Usage:  "The pipeline slug for this cache",
			EnvVar: "BUILDKITE_PIPELINE_SLUG",
		},
		cli.StringFlag{
			Name:   "organization",
			Value:  "",
			Usage:  "The organization slug for this cache",
			EnvVar: "BUILDKITE_ORGANIZATION_SLUG",
		},
		cli.StringFlag{
			Name:   "cache-config-file",
			Value:  ".buildkite/cache.yml",
			Usage:  "Path to the cache configuration YAML file",
			EnvVar: "BUILDKITE_CACHE_CONFIG_FILE",
		},
	}),

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
			result, err := cacheClient.Restore(ctx, cacheID)
			if err != nil {
				return fmt.Errorf("failed to restore cache %q: %w", cacheID, err)
			}

			switch {
			case result.CacheHit:
				// Cache was found and restored
				l.Info("Cache restored", map[string]interface{}{
					"cache_id":          cacheID,
					"cache_key":         result.Key,
					"archive_size":      humanize.Bytes(uint64(result.Archive.Size)),
					"written_bytes":     humanize.Bytes(uint64(result.Archive.WrittenBytes)),
					"written_entries":   fmt.Sprintf("%d", result.Archive.WrittenEntries),
					"compression_ratio": fmt.Sprintf("%.2f", result.Archive.CompressionRatio),
					"transfer_speed":    fmt.Sprintf("%.2fMB/s", result.Transfer.TransferSpeed),
				})
			case result.FallbackUsed:
				// Cache was not found, but a fallback was used
				l.Info("Cache restored (fallback used)", map[string]interface{}{
					"cache_id":          cacheID,
					"cache_key":         result.Key,
					"fallback_used":     result.FallbackUsed,
					"archive_size":      humanize.Bytes(uint64(result.Archive.Size)),
					"written_bytes":     humanize.Bytes(uint64(result.Archive.WrittenBytes)),
					"written_entries":   fmt.Sprintf("%d", result.Archive.WrittenEntries),
					"compression_ratio": fmt.Sprintf("%.2f", result.Archive.CompressionRatio),
					"transfer_speed":    fmt.Sprintf("%.2fMB/s", result.Transfer.TransferSpeed),
				})
			default:
				// Cache was not found
				l.Info("Cache not restored (not found)", map[string]interface{}{
					"cache_id":  cacheID,
					"cache_key": result.Key,
				})
			}
		}

		return nil
	},
}
