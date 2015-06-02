package command

import (
	"github.com/buildkite/agent/buildkite"
	"github.com/buildkite/agent/buildkite/logger"
	"github.com/buildkite/agent/cliconfig"
	"github.com/codegangsta/cli"
)

type MetaDataSetConfig struct {
	Key              string `cli:"arg:0" label:"meta-data key" validate:"required"`
	Value            string `cli:"arg:1" label:"meta-data value validate:"required"`
	Job              string `cli:"job" validate:"required"`
	AgentAccessToken string `cli:"agent-access-token" validate:"required"`
	Endpoint         string `cli:"endpoint" validate:"required"`
	NoColor          bool   `cli:"no-color"`
	Debug            bool   `cli:"debug"`
}

var MetaDataSetHelpDescription = `Usage:

   buildkite-agent meta-data set <key> <value> [arguments...]

Description:

   Set arbitrary data on a build using a basic key/value store.

Example:

   $ buildkite-agent meta-data set "foo" "bar"`

var MetaDataSetCommand = cli.Command{
	Name:        "set",
	Usage:       "Set data on a build",
	Description: MetaDataSetHelpDescription,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:   "job",
			Value:  "",
			Usage:  "Which job should the meta-data be set on",
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
		cfg := MetaDataSetConfig{}

		// Load the configuration
		if err := cliconfig.Load(c, &cfg); err != nil {
			logger.Fatal("%s", err)
		}

		// Setup the any global configuration options
		SetupGlobalConfiguration(cfg)

		var metaData = buildkite.MetaData{
			API: buildkite.API{
				Endpoint: cfg.Endpoint,
				Token:    cfg.AgentAccessToken,
			},
			JobID: cfg.Job,
			Key:   cfg.Key,
			Value: cfg.Value,
		}

		if err := metaData.Set(); err != nil {
			logger.Fatal("Failed to set meta-data: %s", err)
		}
	},
}
