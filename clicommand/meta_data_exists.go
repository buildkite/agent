package clicommand

import (
	"os"
	"time"

	"github.com/buildkite/agent/agent"
	"github.com/buildkite/agent/api"
	"github.com/buildkite/agent/cliconfig"
	"github.com/buildkite/agent/logger"
	"github.com/buildkite/agent/retry"
	"github.com/urfave/cli"
)

var MetaDataExistsHelpDescription = `Usage:

   buildkite-agent meta-data exists <key> [arguments...]

Description:

   The command exits with a status of 0 if the key has been set, or it will
   exit with a status of 100 if the key doesn't exist.

Example:

   $ buildkite-agent meta-data exists "foo"`

type MetaDataExistsConfig struct {
	Key              string `cli:"arg:0" label:"meta-data key" validate:"required"`
	Job              string `cli:"job" validate:"required"`
	AgentAccessToken string `cli:"agent-access-token" validate:"required"`
	Endpoint         string `cli:"endpoint" validate:"required"`
	Scope            string `cli:"scope"`
	NoColor          bool   `cli:"no-color"`
	Debug            bool   `cli:"debug"`
	DebugHTTP        bool   `cli:"debug-http"`
}

var MetaDataExistsCommand = cli.Command{
	Name:        "exists",
	Usage:       "Check to see if the meta data key exists for a build",
	Description: MetaDataExistsHelpDescription,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:   "job",
			Value:  "",
			Usage:  "Which job should the meta-data be checked for",
			EnvVar: "BUILDKITE_JOB_ID",
		},
		cli.StringFlag{
			Name:   "scope",
			Value:  "build",
			Usage:  "What scope should the meta-data be read from, either build (default), branch or pipeline",
			EnvVar: "BUILDKITE_METADATA_SCOPE",
		},
		AgentAccessTokenFlag,
		EndpointFlag,
		NoColorFlag,
		DebugFlag,
		DebugHTTPFlag,
	},
	Action: func(c *cli.Context) {
		// The configuration will be loaded into this struct
		cfg := MetaDataExistsConfig{}

		// Load the configuration
		if err := cliconfig.Load(c, &cfg); err != nil {
			logger.Fatal("%s", err)
		}

		// Setup the any global configuration options
		HandleGlobalFlags(cfg)

		// Create the API client
		client := agent.APIClient{
			Endpoint: cfg.Endpoint,
			Token:    cfg.AgentAccessToken,
		}.Create()

		// Find the meta data value
		var err error
		var exists *api.MetaDataExists
		var resp *api.Response
		err = retry.Do(func(s *retry.Stats) error {
			exists, resp, err = client.MetaData.Exists(cfg.Job, cfg.Key, cfg.Scope)
			if resp != nil && (resp.StatusCode == 401 || resp.StatusCode == 404) {
				s.Break()
			}
			if err != nil {
				logger.Warn("%s (%s)", err, s)
			}

			return err
		}, &retry.Config{Maximum: 10, Interval: 5 * time.Second})
		if err != nil {
			logger.Fatal("Failed to see if meta-data exists: %s", err)
		}

		// If the meta data didn't exist, exit with an error.
		if !exists.Exists {
			os.Exit(100)
		}
	},
}
