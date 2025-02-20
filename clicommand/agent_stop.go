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
	Force bool `cli:"force"`

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

var AgentStopCommand = cli.Command{
	Name:        "stop",
	Usage:       "Stop the agent",
	Description: stopDescription,
	Flags: []cli.Flag{
		cli.BoolFlag{
			Name:  "force",
			Usage: "Cancel any currently running job",
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
		ctx := context.Background()
		ctx, cfg, l, _, done := setupLoggerAndConfig[AgentStopConfig](ctx, c)
		defer done()

		return stop(ctx, cfg, l)
	},
}

func stop(ctx context.Context, cfg AgentStopConfig, l logger.Logger) error {
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

		// Don't bother retrying if the response was one of these statuses
		if resp != nil && (resp.StatusCode == 422) {
			r.Break()
			return err
		}

		// Show the unexpected error
		if err != nil {
			l.Warn("%s (%s)", err, r)
			return err
		}

		l.Info("Successfully stopped agent")
		return nil
	}); err != nil {
		return fmt.Errorf("failed to stop agent: %w", err)
	}

	return nil
}
