package clicommand

import (
	"io/ioutil"
	"os"
	"time"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/cliconfig"
	"github.com/buildkite/agent/v3/retry"
	"github.com/urfave/cli"
)

var StepUpdateHelpDescription = `Usage:

   buildkite-agent step update <attribute> <value> [options...]

Description:

   Update an attribute of a step in the build

Example:

   $ buildkite-agent step update "label" "New Label"
   $ buildkite-agent step update "label" " (add to end of label)" --append
   $ buildkite-agent step update "label" < ./tmp/some-new-label
   $ ./script/label-generator | buildkite-agent step update "label"`

type StepUpdateConfig struct {
	Attribute string `cli:"arg:0" label:"attribute" validate:"required"`
	Value     string `cli:"arg:1" label:"value"`
	Append    bool   `cli:"append"`
	StepOrKey string `cli:"step" validate:"required"`
	Build     string `cli:"build"`

	// Global flags
	Debug       bool     `cli:"debug"`
	NoColor     bool     `cli:"no-color"`
	Experiments []string `cli:"experiment" normalize:"list"`
	Profile     string   `cli:"profile"`

	// API config
	DebugHTTP        bool   `cli:"debug-http"`
	AgentAccessToken string `cli:"agent-access-token" validate:"required"`
	Endpoint         string `cli:"endpoint" validate:"required"`
	NoHTTP2          bool   `cli:"no-http2"`
}

var StepUpdateCommand = cli.Command{
	Name:        "update",
	Usage:       "Change the value of an attribute",
	Description: StepUpdateHelpDescription,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:   "step",
			Value:  "",
			Usage:  "The step to update. Can be either its ID (BUILDKITE_STEP_ID) or key (BUILDKITE_STEP_KEY)",
			EnvVar: "BUILDKITE_STEP_ID",
		},
		cli.StringFlag{
			Name:   "build",
			Value:  "",
			Usage:  "The build to look for the step in. Only required when targeting a step using its key (BUILDKITE_STEP_KEY)",
			EnvVar: "BUILDKITE_BUILD_ID",
		},
		cli.BoolFlag{
			Name:   "append",
			Usage:  "Append to current attribute instead of replacing it",
			EnvVar: "BUILDKITE_STEP_UPDATE_APPEND",
		},

		// API Flags
		AgentAccessTokenFlag,
		EndpointFlag,
		NoHTTP2Flag,
		DebugHTTPFlag,

		// Global flags
		NoColorFlag,
		DebugFlag,
		ExperimentsFlag,
		ProfileFlag,
	},
	Action: func(c *cli.Context) {
		// The configuration will be loaded into this struct
		cfg := StepUpdateConfig{}

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

		// Generate a UUID that will identify this change. We do this
		// outside of the retry loop because we want this UUID to be
		// the same for each attempt at updating the step.
		idempotencyUUID := api.NewUUID()

		// Create the value to update
		update := &api.StepUpdate{
			IdempotencyUUID: idempotencyUUID,
			Build:           cfg.Build,
			Attribute:       cfg.Attribute,
			Value:           cfg.Value,
			Append:          cfg.Append,
		}

		// Post the change
		err := retry.Do(func(s *retry.Stats) error {
			resp, err := client.StepUpdate(cfg.StepOrKey, update)
			if resp != nil && (resp.StatusCode == 400 || resp.StatusCode == 401 || resp.StatusCode == 404) {
				s.Break()
			}
			if err != nil {
				l.Warn("%s (%s)", err, s)
			}

			return err
		}, &retry.Config{Maximum: 10, Interval: 5 * time.Second})
		if err != nil {
			l.Fatal("Failed to change step: %s", err)
		}
	},
}
