package clicommand

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/roko"
	"github.com/urfave/cli/v3"
	"go.opentelemetry.io/otel"
)

const metaDataKeysHelpDescription = `Usage:

    buildkite-agent meta-data keys [options...]

Description:

Lists all meta-data keys that have been previously set, delimited by a newline
and terminated with a trailing newline.

Lists build meta-data by default. Use --scope job to list a job's store.

Example:

    $ buildkite-agent meta-data keys`

type MetaDataKeysConfig struct {
	GlobalConfig
	APIConfig

	Job   string `cli:"job"`
	Build string `cli:"build"`
	Scope string `cli:"scope"`
}

var MetaDataKeysCommand = &cli.Command{
	Name:        "keys",
	Usage:       "Lists all meta-data keys that have been previously set",
	Description: metaDataKeysHelpDescription,
	Flags: slices.Concat(globalFlags(), apiFlags(), []cli.Flag{
		metaDataScopeFlag(),
		&cli.StringFlag{
			Name:    "job",
			Value:   "",
			Usage:   "Which job (with --scope job) or job's build should the meta-data be checked for",
			Sources: cli.EnvVars("BUILDKITE_JOB_ID"),
		},
		&cli.StringFlag{
			Name:    "build",
			Value:   "",
			Usage:   "Which build should the meta-data be retrieved from. --build will take precedence over --job",
			Sources: cli.EnvVars("BUILDKITE_METADATA_BUILD_ID"),
		},
	}),
	Action: func(ctx context.Context, c *cli.Command) error {
		ctx, cfg, l, _, done := setupLoggerAndConfig[MetaDataKeysConfig](ctx, c)
		defer done()
		ctx, span := otel.Tracer("buildkite-agent").Start(ctx, "meta-data-keys")
		defer span.End()

		metadataScope, err := metaDataRequestScope(cfg.Scope, cfg.Build)
		if err != nil {
			return err
		}
		if cfg.Scope == "job" && cfg.Job == "" {
			return fmt.Errorf("--scope job requires --job or BUILDKITE_JOB_ID")
		}

		// Create the API client
		client := api.NewClient(l, loadAPIClientConfig(cfg, "AgentAccessToken"))

		// Find the meta data keys
		targetScope := "job"
		id := cfg.Job

		if cfg.Build != "" {
			targetScope = "build"
			id = cfg.Build
		}

		r := roko.NewRetrier(
			roko.WithMaxAttempts(10),
			roko.WithStrategy(roko.Constant(5*time.Second)),
		)
		keys, err := roko.DoFunc(ctx, r, func(r *roko.Retrier) ([]string, error) {
			keys, resp, err := client.MetaDataKeys(ctx, targetScope, id, metadataScope)
			if api.BreakOnNonRetryable(r, resp, err) {
				return keys, err
			}
			if err != nil {
				l.Warnf("%s (%s)", err, r)
			}
			return keys, err
		})
		if err != nil {
			return fmt.Errorf("failed to find meta-data keys: %w", err)
		}

		for _, key := range keys {
			_, _ = fmt.Fprintf(c.Root().Writer, "%s\n", key)
		}

		return nil
	},
}
