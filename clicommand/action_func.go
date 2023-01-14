package clicommand

import (
	"context"
	"fmt"

	"github.com/buildkite/agent/v3/cliconfig"
	"github.com/buildkite/agent/v3/logger"
	"github.com/urfave/cli"
)

type AgentAction[T any] struct {
	Action func(
		ctx context.Context,
		c *cli.Context,
		l logger.Logger,
		loader cliconfig.Loader,
		cfg *T,
	) error
}

func NewConfigAndLogger[T any](ctx context.Context, cfg *T, f *AgentAction[T]) cli.ActionFunc {
	return func(c *cli.Context) error {
		loader := cliconfig.Loader{
			CLI:                    c,
			Config:                 cfg,
			DefaultConfigFilePaths: DefaultConfigFilePaths(),
		}
		warnings, err := loader.Load()
		if err != nil {
			fmt.Fprintf(c.App.ErrWriter, "%s\n", err)
			return err
		}

		l := CreateLogger(cfg)

		// Now that we have a logger, log out the warnings that loading config generated
		for _, warning := range warnings {
			l.Warn("%s", warning)
		}

		// Setup any global configuration options
		done := HandleGlobalFlags(l, *cfg)
		defer done()

		return f.Action(ctx, c, l, loader, cfg)
	}
}
