package clicommand

import (
	"context"
	"fmt"
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
    $ buildkite-agent step cancel --step "key" --force`

type StepCancelConfig struct {
	StepOrKey string `cli:"step" validate:"required"`
	Force     bool   `cli:"force"`
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

var StepCancelCommand = cli.Command{
	Name:        "cancel",
	Usage:       "Cancel all unfinished jobs for a step",
	Description: stepCancelHelpDescription,
	Flags: []cli.Flag{
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
			Usage:  "Don't wait for the agent to finish before cancelling the jobs",
			EnvVar: "BUILDKITE_STEP_CANCEL_FORCE",
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
		ctx, cfg, l, _, done := setupLoggerAndConfig[StepCancelConfig](context.Background(), c)
		defer done()

		return cancelStep(ctx, cfg, l)
	},
}

func cancelStep(ctx context.Context, cfg StepCancelConfig, l logger.Logger) error {
	// Create the API client
	client := api.NewClient(l, loadAPIClientConfig(cfg, "AgentAccessToken"))

	// Create the value to cancel
	cancel := &api.StepCancel{
		Build: cfg.Build,
		Force: cfg.Force,
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
		return fmt.Errorf("Failed to cancel step: %w", err)
	}

	return nil
}
