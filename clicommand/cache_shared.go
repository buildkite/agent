package clicommand

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/buildkite/agent/v4/internal/cache/configuration"
	"github.com/urfave/cli/v3"
)

const (
	pathCacheName          = "path_cache"
	pathCacheFormatVersion = "path-cache-v1"
)

// CacheConfig includes cache-related shared options for easy inclusion across
// cache command config structs (via embedding).
type CacheConfig struct {
	Names           []string `cli:"name"`
	Registry        string   `cli:"registry"`
	BucketURL       string   `cli:"cache-store-url"`
	CacheConfigFile string   `cli:"cache-config-file"`
	Path            string   `cli:"path"`
	Concurrency     int      `cli:"concurrency"`
}

func cacheFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringSliceFlag{
			Name:    "name",
			Value:   []string{},
			Usage:   "Cache name to process (can be specified multiple times; if empty, processes all caches)",
			Sources: cli.EnvVars("BUILDKITE_CACHE_NAMES"),
		},
		&cli.StringFlag{
			Name:    "registry",
			Value:   "~",
			Usage:   "The slug of the cache registry to use; '~' selects the cluster's default registry",
			Sources: cli.EnvVars("BUILDKITE_AGENT_CACHE_REGISTRY"),
		},
		&cli.StringFlag{
			Name:    "cache-store-url",
			Value:   "",
			Usage:   "The URL of the cache store (e.g., s3://bucket-name); uploads/downloads use ambient credentials",
			Sources: cli.EnvVars("BUILDKITE_AGENT_CACHE_STORE_URL"),
		},
		&cli.StringFlag{
			Name:    "cache-config-file",
			Value:   "",
			Usage:   "Path to the cache configuration YAML file (defaults to .buildkite/cache.yml or .buildkite/cache.yaml)",
			Sources: cli.EnvVars("BUILDKITE_CACHE_CONFIG_FILE"),
		},
		&cli.StringFlag{
			Name:  "path",
			Value: "",
			Usage: "File or directory to cache using the agent's built-in cache configuration",
		},
		&cli.IntFlag{
			Name:    "concurrency",
			Value:   2,
			Usage:   "Number of concurrent cache operations",
			Sources: cli.EnvVars("BUILDKITE_CACHE_CONCURRENCY"),
		},
	}
}

// resolveCacheConfiguration selects one configuration source. Path mode builds
// a single cache definition in memory and deliberately skips default config file
// discovery. File mode preserves the existing explicit/default file behaviour.
func resolveCacheConfiguration(cfg CacheConfig) ([]configuration.Cache, error) {
	if cfg.Path != "" {
		if cfg.CacheConfigFile != "" {
			return nil, fmt.Errorf("--path and --cache-config-file are mutually exclusive")
		}
		if len(cfg.Names) != 0 {
			return nil, fmt.Errorf("--path and --name are mutually exclusive")
		}

		generation, err := resolvePathCacheGeneration(os.Getenv("BUILDKITE_COMMIT"), func() (string, error) {
			output, err := exec.Command("git", "rev-parse", "--verify", "HEAD").Output()
			return string(output), err
		})
		if err != nil {
			return nil, err
		}

		return []configuration.Cache{{
			Name: pathCacheName,
			CacheKey: []configuration.KeyPart{
				{Source: configuration.SourceLiteral, Arg: pathCacheFormatVersion},
				{Source: configuration.SourceAgent, Arg: "os"},
				{Source: configuration.SourceAgent, Arg: "arch", FallbackLimit: true},
				{Source: configuration.SourceLiteral, Arg: generation},
			},
			TargetPaths: []string{cfg.Path},
		}}, nil
	}

	configFile, err := resolveCacheConfigFile(cfg.CacheConfigFile)
	if err != nil {
		return nil, err
	}
	caches, err := configuration.LoadFile(configFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load cache configuration: %w", err)
	}
	return caches, nil
}

// resolvePathCacheGeneration returns a stable generation for the checked-out
// source. Buildkite normally provides the concrete commit; builds configured
// with HEAD and local invocations fall back to the repository's checked-out HEAD.
func resolvePathCacheGeneration(buildkiteCommit string, gitHead func() (string, error)) (string, error) {
	if commit := strings.TrimSpace(buildkiteCommit); commit != "" && commit != "HEAD" {
		return commit, nil
	}

	commit, err := gitHead()
	if err != nil {
		return "", fmt.Errorf("could not determine the checked-out commit for --path cache generation: %w", err)
	}
	if commit = strings.TrimSpace(commit); commit == "" {
		return "", fmt.Errorf("could not determine the checked-out commit for --path cache generation")
	}
	return commit, nil
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
