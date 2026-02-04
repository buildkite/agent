package clicommand

import (
	"context"
	"fmt"
	"slices"

	"github.com/buildkite/agent/v3/internal/cache"
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

Note: This feature is currently in development and subject to change. It is not
yet available to all customers.

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

    dependencies:
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

		if apiCfg.Token == "" {
			return fmt.Errorf("an API token must be provided to save caches")
		}

		// Build cache configuration
		cacheCfg := cache.Config{
			BucketURL:       cfg.BucketURL,
			Branch:          cfg.Branch,
			Pipeline:        cfg.Pipeline,
			Organization:    cfg.Organization,
			CacheConfigFile: cfg.CacheConfigFile,
			Ids:             cfg.Ids,
			APIEndpoint:     apiCfg.Endpoint,
			APIToken:        apiCfg.Token,
			Concurrency:     cfg.Concurrency,
		}

		// Perform cache save (logging happens inside)
		return cache.Save(ctx, l, cacheCfg)
	},
}
