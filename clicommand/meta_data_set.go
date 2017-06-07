package clicommand

import (
	"io/ioutil"
	"os"
	"time"

	"github.com/buildkite/agent/agent"
	"github.com/buildkite/agent/api"
	"github.com/buildkite/agent/cliconfig"
	"github.com/buildkite/agent/logger"
	"github.com/buildkite/agent/retry"
	"github.com/urfave/cli"
)

var MetaDataSetHelpDescription = `Usage:

   buildkite-agent meta-data set <key> [<value>] [arguments...]

Description:

   Set arbitrary data on a build using a basic key/value store.

   You can supply the value as an argument to the command, or pipe in a file or
   script output.

Example:

   $ buildkite-agent meta-data set "foo" "bar"
   $ buildkite-agent meta-data set "foo" < ./tmp/meta-data-value
   $ ./script/meta-data-generator | buildkite-agent meta-data set "foo"`

type MetaDataSetConfig struct {
	Key              string `cli:"arg:0" label:"meta-data key" validate:"required"`
	Value            string `cli:"arg:1" label:"meta-data value"`
	Job              string `cli:"job" validate:"required"`
	AgentAccessToken string `cli:"agent-access-token" validate:"required"`
	Endpoint         string `cli:"endpoint" validate:"required"`
	NoColor          bool   `cli:"no-color"`
	Debug            bool   `cli:"debug"`
	DebugHTTP        bool   `cli:"debug-http"`
}

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
		AgentAccessTokenFlag,
		EndpointFlag,
		NoColorFlag,
		DebugFlag,
		DebugHTTPFlag,
	},
	Action: func(c *cli.Context) {
		// The configuration will be loaded into this struct
		cfg := MetaDataSetConfig{}

		// Load the configuration
		if err := cliconfig.Load(c, &cfg); err != nil {
			logger.Fatal("%s", err)
		}

		// Setup the any global configuration options
		HandleGlobalFlags(cfg)

		// Read the value from STDIN if argument omitted entirely
		if len(c.Args()) < 2 {
			logger.Info("Reading meta-data value from STDIN")

			input, err := ioutil.ReadAll(os.Stdin)
			if err != nil {
				logger.Fatal("Failed to read from STDIN: %s", err)
			}
			cfg.Value = string(input)
		}

		// Create the API client
		client := agent.APIClient{
			Endpoint: cfg.Endpoint,
			Token:    agent.StringToken(cfg.AgentAccessToken),
		}.Create()

		// Create the meta data to set
		metaData := &api.MetaData{
			Key:   cfg.Key,
			Value: cfg.Value,
		}

		// Set the meta data
		err := retry.Do(func(s *retry.Stats) error {
			resp, err := client.MetaData.Set(cfg.Job, metaData)
			if resp != nil && (resp.StatusCode == 401 || resp.StatusCode == 404) {
				s.Break()
			}
			if err != nil {
				logger.Warn("%s (%s)", err, s)
			}

			return err
		}, &retry.Config{Maximum: 10, Interval: 5 * time.Second})
		if err != nil {
			logger.Fatal("Failed to set meta-data: %s", err)
		}
	},
}
