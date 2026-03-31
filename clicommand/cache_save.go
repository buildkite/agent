package clicommand

import (
	"context"
	"fmt"
	"slices"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/internal/cache"
	"github.com/urfave/cli"
	"go.opentelemetry.io/otel"
)

const cacheSaveHelpDescription = `Usage:

    buildkite-agent cache save [options]

Description:

Saves files to the cache for the current build based on the cache configuration
defined in your cache config file (defaults to .buildkite/cache.yml or
.buildkite/cache.yaml).

The cache configuration file defines which files or directories should be cached
and their associated cache key.

Note: This feature is currently in development and subject to change. It is not
yet available to all customers.

Example:

    $ buildkite-agent cache save

This will save all caches defined in the cache configuration file.
You can also save specific caches by providing their names:

    $ buildkite-agent cache save --name "node"

The cache is stored at BUILDKITE_AGENT_CACHE_STORE_URL (or --cache-store-url).
The registry is selected by BUILDKITE_AGENT_CACHE_REGISTRY (or --registry); '~'
selects the cluster's default registry. If an entry already exists at the same
address it is not overwritten.

Configuration File Format:

The cache configuration file should be in YAML format. cache_key is an ordered
list of parts; each part is a literal string or one of { agent: os },
{ agent: arch }, { checksum: <file> }, or { env: <VAR> }:

    caches:
      - name: node
        cache_key:
          - node
          - { agent: os }
          - { agent: arch }
          - { checksum: package-lock.json }
        target_paths:
          - node_modules`

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
		ctx, span := otel.Tracer("buildkite-agent").Start(ctx, "cache-save")
		defer span.End()

		// Emit a Buildkite group header as raw job-log output so the cache
		// output is collapsed into its own group.
		fmt.Println("--- :package: Saving cache...")

		apiCfg := loadAPIClientConfig(cfg, "AgentAccessToken")

		if apiCfg.Token == "" {
			return fmt.Errorf("an API token must be provided to save caches")
		}

		apiClient := api.NewClient(l, apiCfg)

		cacheConfigFile, err := resolveCacheConfigFile(cfg.CacheConfigFile)
		if err != nil {
			return err
		}

		// Build cache configuration
		cacheCfg := cache.Config{
			Registry:        cfg.Registry,
			BucketURL:       cfg.BucketURL,
			CacheConfigFile: cacheConfigFile,
			Names:           cfg.Names,
			Concurrency:     cfg.Concurrency,
		}

		// Perform cache save (logging happens inside)
		return cache.RunSave(ctx, l, apiClient, cacheCfg)
	},
}
