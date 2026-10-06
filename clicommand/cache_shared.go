package clicommand

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/buildkite/agent/v4/internal/cache/archive"
	"github.com/urfave/cli/v3"
)

// CacheConfig includes cache-related shared options for easy inclusion across
// cache command config structs (via embedding).
type CacheConfig struct {
	Names           []string `cli:"name"`
	Registry        string   `cli:"registry"`
	BucketURL       string   `cli:"cache-store-url"`
	CacheConfigFile string   `cli:"cache-config-file"`
	Concurrency     int      `cli:"concurrency"`
	FailOnError     bool     `cli:"cache-fail-on-error"`

	// Experimental (A-1952 benchmarks only).
	ExperimentalArchiveMethod   string   `cli:"experimental-archive-method"`
	ExperimentalTelemetryLabels []string `cli:"experimental-telemetry-label"`
	ExperimentalRusageFile      string   `cli:"experimental-rusage-file"`
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
		&cli.IntFlag{
			Name:    "concurrency",
			Value:   2,
			Usage:   "Number of concurrent cache operations",
			Sources: cli.EnvVars("BUILDKITE_CACHE_CONCURRENCY"),
		},
		&cli.BoolFlag{
			Name:    "cache-fail-on-error",
			Value:   false,
			Usage:   "Fail the command (non-zero exit) when a cache save or restore fails. By default a cache is best-effort: failures are logged and skipped so they never fail the build",
			Sources: cli.EnvVars("BUILDKITE_AGENT_CACHE_FAIL_ON_ERROR"),
		},
		&cli.StringFlag{
			Name:    "experimental-archive-method",
			Usage:   "EXPERIMENTAL, for A-1952 benchmarks only: the ZIP entry method, zstd, zstd_parallel (Zstd entries encoded with bounded concurrent blocks) or store. Save builds the archive with it; save and restore fail unless the archive uses it",
			Sources: cli.EnvVars("BUILDKITE_CACHE_EXPERIMENTAL_ARCHIVE_METHOD"),
			Hidden:  true,
		},
		&cli.StringSliceFlag{
			Name:    "experimental-telemetry-label",
			Usage:   "EXPERIMENTAL, for A-1952 benchmarks only: a label ([a-z0-9_], up to 64 characters) added to the User-Agent as a1952_<label> (can be specified multiple times)",
			Sources: cli.EnvVars("BUILDKITE_CACHE_EXPERIMENTAL_TELEMETRY_LABELS"),
			Hidden:  true,
		},
		&cli.StringFlag{
			Name:    "experimental-rusage-file",
			Usage:   "EXPERIMENTAL, for A-1952 benchmarks only: when the command finishes, write its CPU time and peak memory (getrusage, for the agent and its child processes) to this file as JSON",
			Sources: cli.EnvVars("BUILDKITE_CACHE_EXPERIMENTAL_RUSAGE_FILE"),
			Hidden:  true,
		},
	}
}

var experimentalTelemetryLabelRE = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)

// experimentalCacheUserAgent appends A-1952 benchmark identification to
// userAgent: a1952_method_<method> when an archive method is set, then
// a1952_<label> for each label. Backend request logs record the User-Agent
// alongside cache.stats, so this attributes stats to a method and workload
// without a backend change. Returns userAgent unchanged when neither is set.
func experimentalCacheUserAgent(userAgent string, cfg CacheConfig) (string, error) {
	var tokens []string
	if cfg.ExperimentalArchiveMethod != "" {
		if err := archive.ValidateEntryMethod(cfg.ExperimentalArchiveMethod); err != nil {
			return "", err
		}
		tokens = append(tokens, "a1952_method_"+cfg.ExperimentalArchiveMethod)
	}
	for _, label := range cfg.ExperimentalTelemetryLabels {
		if !experimentalTelemetryLabelRE.MatchString(label) {
			return "", fmt.Errorf("invalid experimental telemetry label %q: want 1-64 characters from [a-z0-9_]", label)
		}
		tokens = append(tokens, "a1952_"+label)
	}
	if len(tokens) == 0 {
		return userAgent, nil
	}
	return userAgent + " " + strings.Join(tokens, " "), nil
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
