package clicommand

import (
	"context"
	"fmt"
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
defined in your cache config file (defaults to .buildkite/cache.yml or
.buildkite/cache.yaml).

The cache configuration file defines which files or directories should be restored
and their associated cache key. An entry is restored when its target_paths
(an order-insensitive set) and the mandatory parts of its cache_key match. By
default every key part is mandatory; mark a part with fallback_limit: true to make
every part after it optional.

Note: This feature is currently in development and subject to change. It is not
yet available to all customers.

Example:

    $ buildkite-agent cache restore

This will restore all caches defined in the cache configuration file.
You can also restore specific caches by providing their names:

    $ buildkite-agent cache restore --name "node"

The cache is retrieved from BUILDKITE_AGENT_CACHE_STORE_URL (or --cache-store-url),
downloaded directly using the agent's ambient credentials. The registry is
selected by BUILDKITE_AGENT_CACHE_REGISTRY (or --registry); '~' selects the
cluster default.

Configuration File Format:

The cache configuration file should be in YAML format. cache_key is an ordered
list of parts; each part is a literal string or one of { agent: os },
{ agent: arch }, { checksum: <file> }, or { env: <VAR> }. Any one part may also
set fallback_limit: true to make every part after it optional for fallback
matching (the marked part itself stays mandatory). In the example below an exact
match is preferred, but if the lockfile changed, an entry matching node + os + arch
is still restored, as the fallback is specified on arch, making node + os + arch mandatory, 
but making checksum optional:

    caches:
      - name: node
        cache_key:
          - node
          - { agent: os }
          - { agent: arch, fallback_limit: true }
          - { checksum: package-lock.json }
        target_paths:
          - node_modules

Cache Restoration Results:

The command will report one of three outcomes for each cache:
  - Cache hit: Exact key match found and restored
  - Fallback used: No exact match, but a fallback key was found and restored
  - Cache miss: No matching cache found`

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

		// Emit a Buildkite group header as raw job-log output so the cache
		// output is collapsed into its own group.
		fmt.Println("--- :package: Restoring cache...")

		apiCfg := loadAPIClientConfig(cfg, "AgentAccessToken")
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
		}

		// Perform cache restore (logging happens inside)
		return cache.RunRestore(ctx, l, apiClient, cacheCfg)
	},
}
