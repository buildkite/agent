package clicommand

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/roko"
	"github.com/urfave/cli"
)

const stepUpdateHelpDescription = `Usage:

    buildkite-agent step update <attribute> <value> [options...]

Description:

Update an attribute of a step in the build

Note that step labels are used in commit status updates, so if you change the
label of a running step, you may end up with an 'orphaned' status update
under the old label, as well as new ones using the updated label.

To avoid orphaned status updates, in your Pipeline Settings > GitHub:

* Make sure Update commit statuses is not selected. Note that this prevents
  Buildkite from automatically creating and sending statuses for this pipeline,
  meaning you will have to handle all commit statuses through the pipeline.yml

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

var StepUpdateCommand = cli.Command{
	Name:        "update",
	Usage:       "Change the value of an attribute",
	Description: stepUpdateHelpDescription,
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
		LogLevelFlag,
		ExperimentsFlag,
		ProfileFlag,
	},
	Action: func(c *cli.Context) error {
		ctx, cfg, l, _, done := setupLoggerAndConfig[StepUpdateConfig](context.Background(), c)
		defer done()

		// Read the value from STDIN if argument omitted entirely
		if len(c.Args()) < 2 {
			l.Info("Reading value from STDIN")

			input, err := io.ReadAll(os.Stdin)
			if err != nil {
				return fmt.Errorf("failed to read from STDIN: %w", err)
			}
			cfg.Value = string(input)
		}

		// Create the API client
		client := api.NewClient(l, loadAPIClientConfig(cfg, "AgentAccessToken"))

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
		if err := roko.NewRetrier(
			roko.WithMaxAttempts(10),
			roko.WithStrategy(roko.Constant(5*time.Second)),
		).DoWithContext(ctx, func(r *roko.Retrier) error {
			resp, err := client.StepUpdate(ctx, cfg.StepOrKey, update)
			if resp != nil && (resp.StatusCode == 400 || resp.StatusCode == 401 || resp.StatusCode == 404) {
				r.Break()
			}
			if err != nil {
				l.Warn("%s (%s)", err, r)
				return err
			}
			return nil
		}); err != nil {
			return fmt.Errorf("failed to change step: %w", err)
		}

		return nil
	},
}
