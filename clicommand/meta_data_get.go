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

const metaDataGetHelpDescription = `Usage:

    buildkite-agent meta-data get <key> [options...]

Description:

Get data from a build's key/value store, or a job's store with --scope job.

Example:

    $ buildkite-agent meta-data get "foo"`

type MetaDataGetConfig struct {
	GlobalConfig
	APIConfig

	Key     string `cli:"arg:0" label:"meta-data key" validate:"required"`
	Default string `cli:"default"`
	Job     string `cli:"job"`
	Build   string `cli:"build"`
	Scope   string `cli:"scope"`
}

var MetaDataGetCommand = &cli.Command{
	Name:        "get",
	Usage:       "Get data from a build or job",
	Description: metaDataGetHelpDescription,
	Flags: slices.Concat(globalFlags(), apiFlags(), []cli.Flag{
		metaDataScopeFlag(),
		&cli.StringFlag{
			Name:  "default",
			Value: "",
			Usage: "If the meta-data value doesn't exist return this instead",
		},
		&cli.StringFlag{
			Name:    "job",
			Value:   "",
			Usage:   "Which job (with --scope job) or job's build should the meta-data be retrieved from",
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
		ctx, cfg, l, _, done := setupLoggerAndConfig[MetaDataGetConfig](ctx, c)
		defer done()
		ctx, span := otel.Tracer("buildkite-agent").Start(ctx, "meta-data-get")
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

		// Find the meta data value

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
		metaData, resp, err := roko.DoFunc2(ctx, r, func(r *roko.Retrier) (*api.MetaData, *api.Response, error) {
			metaData, resp, err := client.GetMetaData(ctx, targetScope, id, cfg.Key, metadataScope)
			if api.BreakOnNonRetryable(r, resp, err) {
				return nil, resp, err
			}
			if err != nil {
				l.Warnf("%s (%s)", err, r)
				return nil, resp, err
			}
			return metaData, resp, nil
		})
		if err != nil {
			// Buildkite returns a 404 if the key doesn't exist. If
			// we get this status, and we've got a default - return
			// that instead and bail early.
			//
			// We also use `IsSet` instead of `cfg.Default != ""`
			// to allow people to use a default of a blank string.
			if resp != nil && resp.StatusCode == 404 && c.IsSet("default") {
				l.Warnf(
					"No meta-data value exists with key %q, returning the supplied default %q",
					cfg.Key,
					cfg.Default,
				)
				_, _ = fmt.Fprintln(c.Root().Writer, cfg.Default)
				return nil
			}

			return fmt.Errorf("failed to get meta-data: %w", err)
		}

		_, err = fmt.Fprintln(c.Root().Writer, metaData.Value)
		return err
	},
}
