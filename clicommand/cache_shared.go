package clicommand

import "github.com/urfave/cli/v3"

// CacheConfig includes cache-related shared options for easy inclusion across
// cache command config structs (via embedding).
type CacheConfig struct {
	Ids             []string `cli:"ids"`
	Registry        string   `cli:"registry"`
	BucketURL       string   `cli:"bucket-url"`
	Branch          string   `cli:"branch" validate:"required"`
	Pipeline        string   `cli:"pipeline" validate:"required"`
	Organization    string   `cli:"organization" validate:"required"`
	CacheConfigFile string   `cli:"cache-config-file"`
	Concurrency     int      `cli:"concurrency"`
}

func cacheFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringSliceFlag{
			Name:    "ids",
			Value:   nil,
			Usage:   "Cache IDs to process (can be specified multiple times; if empty, processes all caches)",
			Sources: cli.EnvVars("BUILDKITE_CACHE_IDS"),
		},
		&cli.StringFlag{
			Name:    "registry",
			Value:   "~",
			Usage:   "The slug of the cache registry to use, defaults to the default registry (~)",
			Sources: cli.EnvVars("BUILDKITE_CACHE_REGISTRY"),
		},
		&cli.StringFlag{
			Name:    "bucket-url",
			Value:   "",
			Usage:   "The URL of the bucket (e.g., s3://bucket-name)",
			Sources: cli.EnvVars("BUILDKITE_CACHE_BUCKET_URL"),
		},
		&cli.StringFlag{
			Name:    "branch",
			Value:   "",
			Usage:   "Which branch should the cache be associated with",
			Sources: cli.EnvVars("BUILDKITE_BRANCH"),
		},
		&cli.StringFlag{
			Name:    "pipeline",
			Value:   "",
			Usage:   "The pipeline slug for this cache",
			Sources: cli.EnvVars("BUILDKITE_PIPELINE_SLUG"),
		},
		&cli.StringFlag{
			Name:    "organization",
			Value:   "",
			Usage:   "The organization slug for this cache",
			Sources: cli.EnvVars("BUILDKITE_ORGANIZATION_SLUG"),
		},
		&cli.StringFlag{
			Name:    "cache-config-file",
			Value:   ".buildkite/cache.yml",
			Usage:   "Path to the cache configuration YAML file",
			Sources: cli.EnvVars("BUILDKITE_CACHE_CONFIG_FILE"),
		},
		&cli.IntFlag{
			Name:    "concurrency",
			Value:   2,
			Usage:   "Number of concurrent cache operations",
			Sources: cli.EnvVars("BUILDKITE_CACHE_CONCURRENCY"),
		},
	}
}
