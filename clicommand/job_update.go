package clicommand

import (
	"io/ioutil"
	"os"
	"time"

	"github.com/buildkite/agent/api"
	"github.com/buildkite/agent/cliconfig"
	"github.com/buildkite/agent/retry"
	"github.com/urfave/cli"
)

var JobUpdateHelpDescription = `Usage:

   buildkite-agent job update <attribute> <value> [arguments...]

Description:

   Update an attribute of a paticular job created from a step.

   By default the command will target the currently running job using the value
   from $BUILDKITE_JOB_ID. You can update a different job by passing a
   different UUID to the --job flag.

   If you want to target all the jobs created from a step (created using
   parallelism), or a non-job step type (i.e. wait or trigger) then use the
   "buildkite-agent step update" command.

Example:

   # Changing the timeout of the current job
   $ buildkite-agent job update "timeout_in_minutes" "30"

   # Extending the current timeout by 10 minutes
   $ buildkite-agent job update "timeout_in_minutes" "10" --append

   # Disable previously enabled manual retries for this job
   $ buildkite-agent job update "retry.manual.allowed" "false"
   $ buildkite-agent job update "retry.manual.reason" "Reason as to why retries were turned off"`

type JobUpdateConfig struct {
	Attribute string `cli:"arg:0" label:"attribute" validate:"required"`
	Value     string `cli:"arg:1" label:"value"`
	Append    bool   `cli:"append"`
	Job       string `cli:"job" validate:"required"`

	// Global flags
	Debug   bool   `cli:"debug"`
	NoColor bool   `cli:"no-color"`
	Profile string `cli:"profile"`

	// API config
	DebugHTTP        bool   `cli:"debug-http"`
	AgentAccessToken string `cli:"agent-access-token" validate:"required"`
	Endpoint         string `cli:"endpoint" validate:"required"`
	NoHTTP2          bool   `cli:"no-http2"`
}

var JobUpdateCommand = cli.Command{
	Name:        "update",
	Usage:       "Update an attribute of a paticular job created from a step",
	Description: JobUpdateHelpDescription,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:   "job",
			Value:  "",
			Usage:  "Target a specific job",
			EnvVar: "BUILDKITE_JOB_ID",
		},
		cli.BoolFlag{
			Name:   "append",
			Usage:  "Append the value to attribute instead of replacing it",
			EnvVar: "BUILDKITE_JOB_UPDATE_APPEND",
		},

		// API Flags
		AgentAccessTokenFlag,
		EndpointFlag,
		NoHTTP2Flag,
		DebugHTTPFlag,

		// Global flags
		NoColorFlag,
		DebugFlag,
		ProfileFlag,
	},
	Action: func(c *cli.Context) {
		// The configuration will be loaded into this struct
		cfg := JobUpdateConfig{}

		l := CreateLogger(&cfg)

		// Load the configuration
		if err := cliconfig.Load(c, l, &cfg); err != nil {
			l.Fatal("%s", err)
		}

		// Setup any global configuration options
		done := HandleGlobalFlags(l, cfg)
		defer done()

		// Read the value from STDIN if argument omitted entirely
		if len(c.Args()) < 2 {
			l.Info("Reading value from STDIN")

			input, err := ioutil.ReadAll(os.Stdin)
			if err != nil {
				l.Fatal("Failed to read from STDIN: %s", err)
			}
			cfg.Value = string(input)
		}

		// Create the API client
		client := api.NewClient(l, loadAPIClientConfig(cfg, `AgentAccessToken`))

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
			resp, err := client.JobUpdate(cfg.Job, update)
			if resp != nil && (resp.StatusCode == 400 || resp.StatusCode == 401 || resp.StatusCode == 404) {
				s.Break()
			}
			if err != nil {
				l.Warn("%s (%s)", err, s)
			}

			return err
		}, &retry.Config{Maximum: 10, Interval: 5 * time.Second})
		if err != nil {
			l.Fatal("Failed to change job: %s", err)
		}
	},
}
