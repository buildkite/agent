package clicommand

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/roko"
	"github.com/urfave/cli"
)

const metaDataGetHelpDescription = `Usage:

    buildkite-agent meta-data get <key> [options...]

Description:

Get data from a build's key/value store.

Example:

    $ buildkite-agent meta-data get "foo"`

type MetaDataGetConfig struct {
	GlobalConfig
	APIConfig

	Key     string `cli:"arg:0" label:"meta-data key" validate:"required"`
	Default string `cli:"default"`
	Job     string `cli:"job"`
	Build   string `cli:"build"`
}

var MetaDataGetCommand = cli.Command{
	Name:        "get",
	Usage:       "Get data from a build",
	Description: metaDataGetHelpDescription,
	Flags: slices.Concat(globalFlags(), apiFlags(), []cli.Flag{
		cli.StringFlag{
			Name:  "default",
			Value: "",
			Usage: "If the meta-data value doesn't exist return this instead",
		},
		cli.StringFlag{
			Name:   "job",
			Value:  "",
			Usage:  "Which job's build should the meta-data be retrieved from",
			EnvVar: "BUILDKITE_JOB_ID",
		},
		cli.StringFlag{
			Name:   "build",
			Value:  "",
			Usage:  "Which build should the meta-data be retrieved from. --build will take precedence over --job",
			EnvVar: "BUILDKITE_METADATA_BUILD_ID",
		},
	}),
	Action: func(c *cli.Context) error {
		ctx := context.Background()
		ctx, cfg, l, _, done := setupLoggerAndConfig[MetaDataGetConfig](ctx, c)
		defer done()

		// Create the API client
		client := api.NewClient(l, loadAPIClientConfig(cfg, "AgentAccessToken"))

		// Find the meta data value

		scope := "job"
		id := cfg.Job

		if cfg.Build != "" {
			scope = "build"
			id = cfg.Build
		}

		r := roko.NewRetrier(
			roko.WithMaxAttempts(10),
			roko.WithStrategy(roko.Constant(5*time.Second)),
		)
		metaData, resp, err := roko.DoFunc2(ctx, r, func(r *roko.Retrier) (*api.MetaData, *api.Response, error) {
			metaData, resp, err := client.GetMetaData(ctx, scope, id, cfg.Key)
			// Don't bother retrying if the response was one of these statuses
			if resp != nil && (resp.StatusCode == 401 || resp.StatusCode == 404 || resp.StatusCode == 400) {
				r.Break()
				return nil, resp, err
			}
			if err != nil {
				l.Warn("%s (%s)", err, r)
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
				l.Warn(
					"No meta-data value exists with key %q, returning the supplied default %q",
					cfg.Key,
					cfg.Default,
				)
				fmt.Fprint(c.App.Writer, cfg.Default)
				return nil
			}

			return fmt.Errorf("failed to get meta-data: %w", err)
		}

		// TODO: in the next agent magor version, we should terminate with a newline using fmt.FPrintln
		_, err = fmt.Fprint(c.App.Writer, metaData.Value)
		return err
	},
}
