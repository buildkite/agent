package clicommand

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/roko"
	"github.com/urfave/cli"
)

const stepCancelHelpDescription = `Usage:

    buildkite-agent step cancel [options...]

Description:

Cancel all unfinished jobs for a step

Example:

    $ buildkite-agent step cancel --step "key"
    $ buildkite-agent step cancel --step "key" --force
    $ buildkite-agent step cancel --step "key" --force --force-grace-period-seconds 30
`

type StepCancelConfig struct {
	GlobalConfig
	APIConfig

	StepOrKey               string `cli:"step" validate:"required"`
	Force                   bool   `cli:"force"`
	ForceGracePeriodSeconds int64  `cli:"force-grace-period-seconds"`
	Build                   string `cli:"build"`
}

var StepCancelCommand = cli.Command{
	Name:        "cancel",
	Usage:       "Cancel all unfinished jobs for a step",
	Description: stepCancelHelpDescription,
	Flags: slices.Concat(globalFlags(), apiFlags(), []cli.Flag{
		cli.StringFlag{
			Name:   "step",
			Value:  "",
			Usage:  "The step to cancel. Can be either its ID (BUILDKITE_STEP_ID) or key (BUILDKITE_STEP_KEY)",
			EnvVar: "BUILDKITE_STEP_ID",
		},
		cli.StringFlag{
			Name:   "build",
			Value:  "",
			Usage:  "The build to look for the step in. Only required when targeting a step using its key (BUILDKITE_STEP_KEY)",
			EnvVar: "BUILDKITE_BUILD_ID",
			Hidden: true,
		},
		cli.BoolFlag{
			Name:   "force",
			Usage:  "Transition unfinished jobs to a canceled state instead of waiting for jobs to finish uploading artifacts (default: false)",
			EnvVar: "BUILDKITE_STEP_CANCEL_FORCE",
		},

		cli.Int64Flag{
			Name:   "force-grace-period-seconds",
			Value:  defaultCancelGracePeriodSecs,
			Usage:  "The number of seconds to wait for agents to finish uploading artifacts before transitioning unfinished jobs to a canceled state. ′--force′ must also be supplied for this to take affect",
			EnvVar: "BUILDKITE_STEP_CANCEL_FORCE_GRACE_PERIOD_SECONDS,BUILDKITE_CANCEL_GRACE_PERIOD",
		},
	}),
	Action: func(c *cli.Context) error {
		ctx, cfg, l, _, done := setupLoggerAndConfig[StepCancelConfig](context.Background(), c)
		defer done()

		if cfg.ForceGracePeriodSeconds < 0 {
			return fmt.Errorf("the value of ′--force-grace-period-seconds′ must be greater than or equal to 0")
		}

		return cancelStep(ctx, cfg, l)
	},
}

func cancelStep(ctx context.Context, cfg StepCancelConfig, l logger.Logger) error {
	// Create the API client
	client := api.NewClient(l, loadAPIClientConfig(cfg, "AgentAccessToken"))

	cancel := &api.StepCancel{
		Build:                   cfg.Build,
		Force:                   cfg.Force,
		ForceGracePeriodSeconds: cfg.ForceGracePeriodSeconds,
	}

	// Post the change
	if err := roko.NewRetrier(
		roko.WithMaxAttempts(10),
		roko.WithStrategy(roko.Constant(5*time.Second)),
	).DoWithContext(ctx, func(r *roko.Retrier) error {
		// Attempt to cancel the step
		stepCancelResponse, resp, err := client.StepCancel(ctx, cfg.StepOrKey, cancel)

		// Don't bother retrying if the response was one of these statuses
		if resp != nil && (resp.StatusCode == 400 || resp.StatusCode == 401 || resp.StatusCode == 404) {
			r.Break()
		}

		// Show the unexpected error
		if err != nil {
			l.Warn("%s (%s)", err, r)
			return err
		}

		l.Info("Successfully cancelled step: %s", stepCancelResponse.UUID)
		return nil
	}); err != nil {
		return fmt.Errorf("failed to cancel step: %w", err)
	}

	return nil
}
