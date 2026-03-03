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

const metaDataKeysHelpDescription = `Usage:

    buildkite-agent meta-data keys [options...]

Description:

Lists all meta-data keys that have been previously set, delimited by a newline
and terminated with a trailing newline.

Example:

    $ buildkite-agent meta-data keys`

type MetaDataKeysConfig struct {
	GlobalConfig
	APIConfig

	Job   string `cli:"job"`
	Build string `cli:"build"`
}

var MetaDataKeysCommand = cli.Command{
	Name:        "keys",
	Usage:       "Lists all meta-data keys that have been previously set",
	Description: metaDataKeysHelpDescription,
	Flags: slices.Concat(globalFlags(), apiFlags(), []cli.Flag{
		cli.StringFlag{
			Name:   "job",
			Value:  "",
			Usage:  "Which job's build should the meta-data be checked for",
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
		ctx, cfg, l, _, done := setupLoggerAndConfig[MetaDataKeysConfig](ctx, c)
		defer done()

		// Create the API client
		client := api.NewClient(l, loadAPIClientConfig(cfg, "AgentAccessToken"))

		// Find the meta data keys
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
		keys, err := roko.DoFunc(ctx, r, func(r *roko.Retrier) ([]string, error) {
			keys, resp, err := client.MetaDataKeys(ctx, scope, id)
			if resp != nil && (resp.StatusCode == 401 || resp.StatusCode == 404) {
				r.Break()
			}
			if err != nil {
				l.Warn("%s (%s)", err, r)
			}
			return keys, err
		})
		if err != nil {
			return fmt.Errorf("failed to find meta-data keys: %w", err)
		}

		for _, key := range keys {
			fmt.Fprintf(c.App.Writer, "%s\n", key) //nolint:errcheck // CLI output; errors are non-actionable
		}

		return nil
	},
}
