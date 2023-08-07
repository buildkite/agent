package clicommand

import (
	"context"
	"fmt"
	"time"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/roko"
	"github.com/urfave/cli"
)

const metaDataGetHelpDescription = `Usage:

   buildkite-agent meta-data get <key> [options...]

Description:

   Get data from a builds key/value store.

Example:

   $ buildkite-agent meta-data get "foo"`

type MetaDataGetConfig struct {
	Key     string `cli:"arg:0" label:"meta-data key" validate:"required"`
	Default string `cli:"default"`
	Job     string `cli:"job"`
	Build   string `cli:"build"`

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

var MetaDataGetCommand = cli.Command{
	Name:        "get",
	Usage:       "Get data from a build",
	Description: metaDataGetHelpDescription,
	Flags: []cli.Flag{
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
	Action: func(c *cli.Context) {
		ctx := context.Background()
		cfg, l, _, done := setupLoggerAndConfig[MetaDataGetConfig](c)
		defer done()

		// Create the API client
		client := api.NewClient(l, loadAPIClientConfig(cfg, "AgentAccessToken"))

		// Find the meta data value
		var metaData *api.MetaData
		var resp *api.Response

		scope := "job"
		id := cfg.Job

		if cfg.Build != "" {
			scope = "build"
			id = cfg.Build
		}

		if err := roko.NewRetrier(
			roko.WithMaxAttempts(10),
			roko.WithStrategy(roko.Constant(5*time.Second)),
		).DoWithContext(ctx, func(r *roko.Retrier) error {
			var err error
			metaData, resp, err = client.GetMetaData(ctx, scope, id, cfg.Key)
			// Don't bother retrying if the response was one of these statuses
			if resp != nil && (resp.StatusCode == 401 || resp.StatusCode == 404 || resp.StatusCode == 400) {
				r.Break()
				return err
			}
			if err != nil {
				l.Warn("%s (%s)", err, r)
				return err
			}
			return nil
		}); err != nil {
			// Buildkite returns a 404 if the key doesn't exist. If
			// we get this status, and we've got a default - return
			// that instead and bail early.
			//
			// We also use `IsSet` instead of `cfg.Default != ""`
			// to allow people to use a default of a blank string.
			if resp.StatusCode == 404 && c.IsSet("default") {
				l.Warn("No meta-data value exists with key `%s`, returning the supplied default \"%s\"", cfg.Key, cfg.Default)
				fmt.Fprint(c.App.Writer, cfg.Default)
				return
			}

			l.Panic("Failed to get meta-data: %s", err)
		}

		// Output the value to STDOUT
		fmt.Fprint(c.App.Writer, metaData.Value)
	},
}
