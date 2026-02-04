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

const pauseDescription = `Usage:

    buildkite-agent pause [options...]

Description:

Pauses the current agent.

Example:

    # Pauses the agent
    $ buildkite-agent pause

    # Pauses the agent with an explanatory note and a custom timeout
    $ buildkite-agent pause --note 'too many llamas' --timeout-in-minutes 60`

type AgentPauseConfig struct {
	GlobalConfig
	APIConfig

	Note             string `cli:"note"`
	TimeoutInMinutes int    `cli:"timeout-in-minutes"`
}

var AgentPauseCommand = cli.Command{
	Name:        "pause",
	Category:    categoryJobCommands,
	Usage:       "Pause the agent",
	Description: pauseDescription,
	Flags: slices.Concat(globalFlags(), apiFlags(), []cli.Flag{
		cli.StringFlag{
			Name:   "note",
			Usage:  "A descriptive note to record why the agent is paused",
			EnvVar: "BUILDKITE_AGENT_PAUSE_NOTE",
		},
		cli.IntFlag{
			Name:   "timeout-in-minutes",
			Usage:  "Timeout after which the agent is automatically resumed, in minutes",
			EnvVar: "BUILDKITE_AGENT_PAUSE_TIMEOUT_MINUTES",
		},
	}),
	Action: func(c *cli.Context) error {
		ctx := context.Background()
		ctx, cfg, l, _, done := setupLoggerAndConfig[AgentPauseConfig](ctx, c)
		defer done()

		return pause(ctx, cfg, l)
	},
}

func pause(ctx context.Context, cfg AgentPauseConfig, l logger.Logger) error {
	// Create the API client
	client := api.NewClient(l, loadAPIClientConfig(cfg, "AgentAccessToken"))

	// Retry the agent pause a few times before giving up
	if err := roko.NewRetrier(
		roko.WithMaxAttempts(5),
		roko.WithStrategy(roko.Constant(1*time.Second)),
		roko.WithJitter(),
	).DoWithContext(ctx, func(r *roko.Retrier) error {
		// Attempt to pause the agent
		resp, err := client.Pause(ctx, &api.AgentPauseRequest{
			Note:             cfg.Note,
			TimeoutInMinutes: cfg.TimeoutInMinutes,
		})

		// Don't bother retrying if the response was one of these statuses
		if resp != nil && resp.StatusCode == 422 {
			r.Break()
			return err
		}

		// Show the unexpected error
		if err != nil {
			l.Warn("%s (%s)", err, r)
			return err
		}

		l.Info("Successfully paused agent")
		return nil
	}); err != nil {
		return fmt.Errorf("failed to pause agent: %w", err)
	}

	return nil
}
