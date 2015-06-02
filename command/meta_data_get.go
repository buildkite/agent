package command

import (
	"fmt"
	"github.com/buildkite/agent/buildkite"
	"github.com/buildkite/agent/cliconfig"
	"github.com/buildkite/agent/logger"
	"github.com/codegangsta/cli"
)

type MetaDataGetConfig struct {
	Key              string `cli:"arg:0" label:"meta-data key" validate:"required"`
	Job              string `cli:"job" validate:"required"`
	AgentAccessToken string `cli:"agent-access-token" validate:"required"`
	Endpoint         string `cli:"endpoint" validate:"required"`
	NoColor          bool   `cli:"no-color"`
	Debug            bool   `cli:"debug"`
}

var MetaDataGetHelpDescription = `Usage:

   buildkite-agent meta-data get <key> [arguments...]

Description:

   Get data from a builds key/value store.

Example:

   $ buildkite-agent meta-data get "foo"`

var MetaDataGetCommand = cli.Command{
	Name:        "get",
	Usage:       "Get data from a build",
	Description: MetaDataGetHelpDescription,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:   "job",
			Value:  "",
			Usage:  "Which job should the meta-data be retrieved from",
			EnvVar: "BUILDKITE_JOB_ID",
		},
		cli.StringFlag{
			Name:   "agent-access-token",
			Value:  "",
			Usage:  "The access token used to identify the agent",
			EnvVar: "BUILDKITE_AGENT_ACCESS_TOKEN",
		},
		cli.StringFlag{
			Name:   "endpoint",
			Value:  DefaultEndpoint,
			Usage:  "The Agent API endpoint",
			EnvVar: "BUILDKITE_AGENT_ENDPOINT",
		},
		cli.BoolFlag{
			Name:   "debug",
			Usage:  "Enable debug mode",
			EnvVar: "BUILDKITE_AGENT_DEBUG",
		},
		cli.BoolFlag{
			Name:   "no-color",
			Usage:  "Don't show colors in logging",
			EnvVar: "BUILDKITE_AGENT_NO_COLOR",
		},
	},
	Action: func(c *cli.Context) {
		// The configuration will be loaded into this struct
		cfg := MetaDataGetConfig{}

		// Load the configuration
		if err := cliconfig.Load(c, &cfg); err != nil {
			logger.Fatal("%s", err)
		}

		// Setup the any global configuration options
		SetupGlobalConfiguration(cfg)

		metaData := buildkite.MetaData{
			API: buildkite.API{
				Endpoint: cfg.Endpoint,
				Token:    cfg.AgentAccessToken,
			},
			JobID: cfg.Job,
			Key:   cfg.Key,
		}

		if err := metaData.Get(); err != nil {
			logger.Fatal("Failed to get meta-data: %s", err)
		}

		// Output the value to STDOUT
		fmt.Print(metaData.Value)
	},
}
