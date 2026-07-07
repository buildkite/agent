package clicommand

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/urfave/cli"
)

// CacheConfig includes cache-related shared options for easy inclusion across
// cache command config structs (via embedding).
type CacheConfig struct {
	Names           []string `cli:"name"`
	Registry        string   `cli:"registry"`
	BucketURL       string   `cli:"cache-store-url"`
	CacheConfigFile string   `cli:"cache-config-file"`
	Concurrency     int      `cli:"concurrency"`
}

func cacheFlags() []cli.Flag {
	return []cli.Flag{
		cli.StringSliceFlag{
			Name:   "name",
			Value:  &cli.StringSlice{},
			Usage:  "Cache name to process (can be specified multiple times; if empty, processes all caches)",
			EnvVar: "BUILDKITE_CACHE_NAMES",
		},
		cli.StringFlag{
			Name:   "registry",
			Value:  "~",
			Usage:  "The slug of the cache registry to use; '~' selects the cluster's default registry",
			EnvVar: "BUILDKITE_AGENT_CACHE_REGISTRY",
		},
		cli.StringFlag{
			Name:   "cache-store-url",
			Value:  "",
			Usage:  "The URL of the cache store (e.g., s3://bucket-name); uploads/downloads use ambient credentials",
			EnvVar: "BUILDKITE_AGENT_CACHE_STORE_URL",
		},
		cli.StringFlag{
			Name:   "cache-config-file",
			Value:  "",
			Usage:  "Path to the cache configuration YAML file (defaults to .buildkite/cache.yml or .buildkite/cache.yaml)",
			EnvVar: "BUILDKITE_CACHE_CONFIG_FILE",
		},
		cli.IntFlag{
			Name:   "concurrency",
			Value:  2,
			Usage:  "Number of concurrent cache operations",
			EnvVar: "BUILDKITE_CACHE_CONCURRENCY",
		},
	}
}

// defaultCacheConfigPaths lists the candidate cache configuration files, in
// precedence order, used when --cache-config-file is not provided.
var defaultCacheConfigPaths = []string{
	filepath.FromSlash(".buildkite/cache.yml"),
	filepath.FromSlash(".buildkite/cache.yaml"),
}

// resolveCacheConfigFile returns the cache configuration path to load. An
// explicitly provided path is used as-is. Otherwise it searches the default
// locations, returning the one that exists and erroring if more than one, or
// none, are present.
func resolveCacheConfigFile(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}

	var exists []string
	for _, path := range defaultCacheConfigPaths {
		if _, err := os.Stat(path); err == nil {
			exists = append(exists, path)
		}
	}

	switch len(exists) {
	case 0:
		return "", fmt.Errorf("could not find a default cache configuration file; tried %s", strings.Join(defaultCacheConfigPaths, ", "))
	case 1:
		return exists[0], nil
	default:
		return "", fmt.Errorf("found multiple cache configuration files: %s; please keep only 1 configuration file present", strings.Join(exists, ", "))
	}
}
