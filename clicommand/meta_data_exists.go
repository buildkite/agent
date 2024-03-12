package clicommand

import (
	"context"
	"fmt"
	"time"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/roko"
	"github.com/urfave/cli"
)

const metaDataExistsHelpDescription = `Usage:

    buildkite-agent meta-data exists <key> [options...]

Description:

The command exits with a status of 0 if the key has been set, or it will
exit with a status of 100 if the key doesn't exist.

Example:

    $ buildkite-agent meta-data exists "foo"`

type MetaDataExistsConfig struct {
	Key   string `cli:"arg:0" label:"meta-data key" validate:"required"`
	Job   string `cli:"job"`
	Build string `cli:"build"`

	// Global flags
	Debug       bool     `cli:"debug"`
	LogLevel    string   `cli:"log-level"`
	NoColor     bool     `cli:"no-color"`
	Experiments []string `cli:"experiment" normalize:"list"`
	Profile     string   `cli:"profile"`

	// API config
	DebugHTTP        bool   `cli:"debug-http"`
	AgentAccessToken string `cli:"agent-access-token" validate:"required"`
	Endpoint         string `cli:"endpoint" validate:"required"`
	NoHTTP2          bool   `cli:"no-http2"`
}

var MetaDataExistsCommand = cli.Command{
	Name:        "exists",
	Usage:       "Check to see if the meta data key exists for a build",
	Description: metaDataExistsHelpDescription,
	Flags: []cli.Flag{
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

		// API Flags
		AgentAccessTokenFlag,
		EndpointFlag,
		NoHTTP2Flag,
		DebugHTTPFlag,

		// Global flags
		NoColorFlag,
		DebugFlag,
		LogLevelFlag,
		ExperimentsFlag,
		ProfileFlag,
	},
	Action: func(c *cli.Context) error {
		ctx := context.Background()
		ctx, cfg, l, _, done := setupLoggerAndConfig[MetaDataExistsConfig](ctx, c)
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
		exists, err := roko.DoFunc(ctx, r, func(r *roko.Retrier) (*api.MetaDataExists, error) {
			exists, resp, err := client.ExistsMetaData(ctx, scope, id, cfg.Key)
			if resp != nil && (resp.StatusCode == 401 || resp.StatusCode == 404) {
				r.Break()
			}
			if err != nil {
				l.Warn("%s (%s)", err, r)
				return nil, err
			}
			return exists, nil
		})
		if err != nil {
			return fmt.Errorf("failed to see if meta-data exists: %w", err)
		}

		if !exists.Exists {
			return &SilentExitError{code: 100}
		}

		return nil
	},
}
