package clicommand

import (
	"context"
	"slices"

	"github.com/buildkite/agent/v3/internal/cache"
	"github.com/urfave/cli"
)

const cacheRestoreHelpDescription = `Usage:

    buildkite-agent cache restore [options]

Description:

Restores files from the cache for the current job based on the cache configuration
defined in your cache config file (defaults to .buildkite/cache.yml).

The cache configuration file defines which files or directories should be restored
and their associated cache keys. Caches are scoped by organization, pipeline, and
branch. If an exact cache match is not found, the command will attempt to use
fallback keys if defined in your cache configuration.

Note: This feature is currently in development and subject to change. It is not
yet available to all customers.

Example:

    $ buildkite-agent cache restore

This will restore all caches defined in .buildkite/cache.yml. You can also restore
specific caches by providing their IDs:

    $ buildkite-agent cache restore --ids "node"

The cache will be retrieved from the bucket specified by --bucket-url or your
cache configuration.

Configuration File Format:

The cache configuration file should be in YAML format:

    dependencies:
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
		}

		// Perform cache restore (logging happens inside)
		return cache.Restore(ctx, l, cacheCfg)
	},
}
