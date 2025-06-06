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

const resumeDescription = `Usage:

    buildkite-agent resume [options...]

Description:

Resumes the current agent if it is paused.

Example:

    # Resumes the agent
    $ buildkite-agent resume`

type AgentResumeConfig struct {
	GlobalConfig
	APIConfig
}

var AgentResumeCommand = cli.Command{
	Name:        "resume",
	Category:    categoryJobCommands,
	Usage:       "Resume the agent",
	Description: resumeDescription,
	Flags:       slices.Concat(globalFlags(), apiFlags()),
	Action: func(c *cli.Context) error {
		ctx := context.Background()
		ctx, cfg, l, _, done := setupLoggerAndConfig[AgentResumeConfig](ctx, c)
		defer done()

		return resume(ctx, cfg, l)
	},
}

func resume(ctx context.Context, cfg AgentResumeConfig, l logger.Logger) error {
	// Create the API client
	client := api.NewClient(l, loadAPIClientConfig(cfg, "AgentAccessToken"))

	// Retry the agent resume a few times before giving up
	if err := roko.NewRetrier(
		roko.WithMaxAttempts(5),
		roko.WithStrategy(roko.Constant(1*time.Second)),
		roko.WithJitter(),
	).DoWithContext(ctx, func(r *roko.Retrier) error {
		// Attempt to resume the agent
		resp, err := client.Resume(ctx)

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

		l.Info("Successfully resumed agent")
		return nil
	}); err != nil {
		return fmt.Errorf("failed to resume agent: %w", err)
	}

	return nil
}
