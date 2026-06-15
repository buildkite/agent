package clicommand

import (
	"context"
	"slices"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/internal/cache"
	"github.com/urfave/cli"
	"go.opentelemetry.io/otel"
)

const cacheRestoreHelpDescription = `Usage:

    buildkite-agent cache restore [options]

Description:

Restores files from the cache for the current job based on the cache configuration
defined in your cache config file (defaults to .buildkite/cache.yml).

The cache configuration file defines which files or directories should be restored
and their associated cache key. An entry is restored when its target_paths
(an order-insensitive set) and cache_key match exactly. Fallback matching is not
enabled yet — every key part is treated as mandatory.

Note: This feature is currently in development and subject to change. It is not
yet available to all customers.

Example:

    $ buildkite-agent cache restore

This will restore all caches defined in .buildkite/cache.yml. You can also restore
specific caches by providing their IDs:

    $ buildkite-agent cache restore --names "node"

The cache is retrieved from BUILDKITE_AGENT_CACHE_STORE_URL (or --cache-store-url),
downloaded directly using the agent's ambient credentials. The registry is
selected by BUILDKITE_AGENT_CACHE_REGISTRY (or --registry); '~' selects the
cluster default.

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
		ctx, span := otel.Tracer("buildkite-agent").Start(ctx, "cache-restore")
		defer span.End()

		l.Infof("Cache restore command executed")

		apiCfg := loadAPIClientConfig(cfg, "AgentAccessToken")
		apiClient := api.NewClient(l, apiCfg)

		// Build cache configuration
		cacheCfg := cache.Config{
			Registry:        cfg.Registry,
			BucketURL:       cfg.BucketURL,
			Branch:          cfg.Branch,
			Pipeline:        cfg.Pipeline,
			Organization:    cfg.Organization,
			CacheConfigFile: cfg.CacheConfigFile,
			Names:           cfg.Names,
		}

		// Perform cache restore (logging happens inside)
		return cache.RunRestore(ctx, l, apiClient, cacheCfg)
	},
}
