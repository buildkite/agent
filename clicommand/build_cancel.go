package clicommand

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/logger"
	"github.com/buildkite/roko"
	"github.com/urfave/cli"
)

const buildCancelDescription = `Usage:

    buildkite-agent build cancel [options...]

Description:

Cancel a running build.

Example:

    # Cancels the current build
    $ buildkite-agent build cancel`

type BuildCancelConfig struct {
	GlobalConfig
	APIConfig

	Build string `cli:"build" validate:"required"`
}

var BuildCancelCommand = cli.Command{
	Name:        "cancel",
	Usage:       "Cancel a build",
	Description: buildCancelDescription,
	Flags: slices.Concat(globalFlags(), apiFlags(), []cli.Flag{
		cli.StringFlag{
			Name:   "build",
			Value:  "",
			Usage:  "The build UUID to cancel",
			EnvVar: "BUILDKITE_BUILD_ID",
		},
	}),
	Action: func(c *cli.Context) error {
		ctx := context.Background()
		ctx, cfg, l, _, done := setupLoggerAndConfig[BuildCancelConfig](ctx, c)
		defer done()

		return cancelBuild(ctx, cfg, l)
	},
}

func cancelBuild(ctx context.Context, cfg BuildCancelConfig, l logger.Logger) error {
	// Create the API client
	client := api.NewClient(l, loadAPIClientConfig(cfg, "AgentAccessToken"))

	// Retry the build cancellation a few times before giving up
	if err := roko.NewRetrier(
		roko.WithMaxAttempts(5),
		roko.WithStrategy(roko.Constant(1*time.Second)),
		roko.WithJitter(),
	).DoWithContext(ctx, func(r *roko.Retrier) error {
		// Attempt to cancel the build
		build, resp, err := client.CancelBuild(ctx, cfg.Build)

		if api.BreakOnNonRetryable(r, resp, err) {
			return err
		}
		if err != nil {
			l.Warnf("%s (%s)", err, r)
			return err
		}

		l.Infof("Successfully cancelled build %s", build.UUID)
		return nil
	}); err != nil {
		return fmt.Errorf("failed to cancel build: %w", err)
	}

	return nil
}
