package clicommand

import (
	"fmt"
	"time"

	"github.com/buildkite/agent/api"
	"github.com/buildkite/agent/cliconfig"
	"github.com/buildkite/agent/logger"
	"github.com/buildkite/agent/retry"
	"github.com/urfave/cli"
)

var MetaDataGetHelpDescription = `Usage:

   buildkite-agent meta-data get <key> [arguments...]

Description:

   Get data from a builds key/value store.

Example:

   $ buildkite-agent meta-data get "foo"`

type MetaDataGetConfig struct {
	Key              string `cli:"arg:0" label:"meta-data key" validate:"required"`
	Default          string `cli:"default"`
	Job              string `cli:"job" validate:"required"`
	AgentAccessToken string `cli:"agent-access-token"`
	AgentSocket      string `cli:"agent-socket"`
	Endpoint         string `cli:"endpoint"`
	NoColor          bool   `cli:"no-color"`
	Debug            bool   `cli:"debug"`
	DebugHTTP        bool   `cli:"debug-http"`
}

var MetaDataGetCommand = cli.Command{
	Name:        "get",
	Usage:       "Get data from a build",
	Description: MetaDataGetHelpDescription,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:  "default",
			Value: "",
			Usage: "If the meta-data value doesn't exist return this instead",
		},
		cli.StringFlag{
			Name:   "job",
			Value:  "",
			Usage:  "Which job should the meta-data be retrieved from",
			EnvVar: "BUILDKITE_JOB_ID",
		},
		AgentAccessTokenFlag,
		EndpointFlag,
		AgentSocketFlag,
		NoColorFlag,
		DebugFlag,
		DebugHTTPFlag,
	},
	Action: func(c *cli.Context) {
		// The configuration will be loaded into this struct
		cfg := MetaDataGetConfig{}

		// Load the configuration
		if err := cliconfig.Load(c, &cfg); err != nil {
			logger.Fatal("%s", err)
		}

		// Setup the any global configuration options
		HandleGlobalFlags(cfg)

		// Create the API client
		client := CreateAPIClientFromConfig(cfg)

		// Find the meta data value
		var metaData *api.MetaData
		var err error
		var resp *api.Response
		err = retry.Do(func(s *retry.Stats) error {
			metaData, resp, err = client.MetaData.Get(cfg.Job, cfg.Key)
			// Don't bother retrying if the response was one of these statuses
			if resp != nil && (resp.StatusCode == 401 || resp.StatusCode == 404 || resp.StatusCode == 400) {
				s.Break()
				return err
			}
			if err != nil {
				logger.Warn("%s (%s)", err, s)
			}

			return err
		}, &retry.Config{Maximum: 10, Interval: 5 * time.Second})

		// Deal with the error if we got one
		if err != nil {
			// Buildkite returns a 404 if the key doesn't exist. If
			// we get this status, and we've got a default - return
			// that instead and bail early.
			//
			// We also use `IsSet` instead of `cfg.Default != ""`
			// to allow people to use a default of a blank string.
			if resp.StatusCode == 404 && c.IsSet("default") {
				logger.Warn("No meta-data value exists with key `%s`, returning the supplied default \"%s\"", cfg.Key, cfg.Default)

				fmt.Print(cfg.Default)
				return
			} else {
				logger.Fatal("Failed to get meta-data: %s", err)
			}
		}

		// Output the value to STDOUT
		fmt.Print(metaData.Value)
	},
}
