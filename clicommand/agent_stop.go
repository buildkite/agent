package clicommand

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/roko"
	"github.com/urfave/cli"
)

const stopDescription = `Usage:

    buildkite-agent stop [options...]

Description:

Stop the current agent.

Example:

    # Stops the agent gracefully after any currently running job completes
    $ buildkite-agent stop

    # Stops the agent, cancelling any currently running job
    $ buildkite-agent stop --force`

type AgentStopConfig struct {
	GlobalConfig
	APIConfig

	Force bool `cli:"force"`
}

var AgentStopCommand = cli.Command{
	Name:        "stop",
	Category:    categoryJobCommands,
	Usage:       "Stop the agent",
	Description: stopDescription,
	Flags: slices.Concat(globalFlags(), apiFlags(), []cli.Flag{
		cli.BoolFlag{
			Name:  "force",
			Usage: "Cancel any currently running job (default: false)",
		},
	}),
	Action: func(c *cli.Context) error {
		ctx := context.Background()
		ctx, cfg, l, _, done := setupLoggerAndConfig[AgentStopConfig](ctx, c)
		defer done()

		return stop(ctx, cfg, l)
	},
}

func stop(ctx context.Context, cfg AgentStopConfig, l *slog.Logger) error {
	// Create the API client
	client := api.NewClient(l, loadAPIClientConfig(cfg, "AgentAccessToken"))

	// Retry the build cancellation a few times before giving up
	if err := roko.NewRetrier(
		roko.WithMaxAttempts(5),
		roko.WithStrategy(roko.Constant(1*time.Second)),
		roko.WithJitter(),
	).DoWithContext(ctx, func(r *roko.Retrier) error {
		// Attempt to cancel the build
		resp, err := client.Stop(ctx, &api.AgentStopRequest{
			Force: cfg.Force,
		})

		if api.BreakOnNonRetryable(r, resp, err) {
			return err
		}
		if err != nil {
			l.Warn(fmt.Sprintf("%s (%s)", err, r))
			return err
		}

		l.Info("Successfully stopped agent")
		return nil
	}); err != nil {
		return fmt.Errorf("failed to stop agent: %w", err)
	}

	return nil
}
