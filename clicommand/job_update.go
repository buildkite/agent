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

var JobUpdateHelpDescription = `Usage:

   buildkite-agent job update <attribute> <value> [arguments...]

Description:

   Update an attribute of a job

Example:

   $ buildkite-agent job update "label" "New Label"
   $ buildkite-agent job update "label" " (add to end of label)" --append
   $ buildkite-agent job update "label" < ./tmp/some-new-label
   $ ./script/label-generator | buildkite-agent job update "label"`

type JobUpdateConfig struct {
	Attribute        string `cli:"arg:0" label:"attribute" validate:"required"`
	Value            string `cli:"arg:1" label:"value" validate:"required"`
	Append           bool   `cli:"append"`
	Job              string `cli:"job" validate:"required"`
	AgentAccessToken string `cli:"agent-access-token" validate:"required"`
	Endpoint         string `cli:"endpoint" validate:"required"`
	NoColor          bool   `cli:"no-color"`
	Debug            bool   `cli:"debug"`
	DebugHTTP        bool   `cli:"debug-http"`
}

var JobUpdateCommand = cli.Command{
	Name:        "update",
	Usage:       "Change an attribute on a job",
	Description: JobUpdateHelpDescription,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:   "job",
			Value:  "",
			Usage:  "Which job should the change be made to",
			EnvVar: "BUILDKITE_JOB_ID",
		},
		cli.BoolFlag{
			Name:   "append",
			Usage:  "Append to current attribute instead of replacing it",
			EnvVar: "BUILDKITE_JOB_UPDATE_APPEND",
		},
		AgentAccessTokenFlag,
		EndpointFlag,
		NoColorFlag,
		DebugFlag,
		DebugHTTPFlag,
	},
	Action: func(c *cli.Context) {
		// The configuration will be loaded into this struct
		cfg := JobUpdateConfig{}

		// Load the configuration
		if err := cliconfig.Load(c, &cfg); err != nil {
			logger.Fatal("%s", err)
		}

		// Setup the any global configuration options
		HandleGlobalFlags(cfg)

		// Read the value from STDIN if argument omitted entirely
		if len(c.Args()) < 2 {
			logger.Info("Reading value from STDIN")

			input, err := ioutil.ReadAll(os.Stdin)
			if err != nil {
				logger.Fatal("Failed to read from STDIN: %s", err)
			}
			cfg.Value = string(input)
		}

		// Create the API client
		client := agent.APIClient{
			Endpoint: cfg.Endpoint,
			Token:    cfg.AgentAccessToken,
		}.Create()

		// Generate a UUID that will identifiy this change. We do this
		// outside of the retry loop because we want this UUID to be
		// the same for each attempt at updating the job.
		uuid := api.NewUUID()

		// Create the value to update
		update := &api.JobUpdate{
			UUID:      uuid,
			Attribute: cfg.Attribute,
			Value:     cfg.Value,
			Append:    cfg.Append,
		}

		// Post the change
		err := retry.Do(func(s *retry.Stats) error {
			resp, err := client.Jobs.Update(cfg.Job, update)
			if resp != nil && (resp.StatusCode == 401 || resp.StatusCode == 404) {
				s.Break()
			}
			if err != nil {
				logger.Warn("%s (%s)", err, s)
			}

			return err
		}, &retry.Config{Maximum: 10, Interval: 5 * time.Second})
		if err != nil {
			logger.Fatal("Failed to change job: %s", err)
		}
	},
}
