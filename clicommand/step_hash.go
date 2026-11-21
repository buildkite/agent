package clicommand

import (
	"context"
	"fmt"
	"slices"

	"github.com/buildkite/agent/v4/internal/reuse"
	"github.com/urfave/cli/v3"
)

const stepHashFilesHelpDescription = `Usage:

    buildkite-agent step hash files <pattern>... [--exclude <pattern>]... [--untracked]

Description:

Prints the step reuse digest of the committed files in the current directory's
git repository that match any of the given glob patterns, and none of the
exclude patterns. Patterns use the same syntax as ′if_changed′. It fails if any
pattern matches no such files.

This is the same digest that ′buildkite-agent pipeline upload′ computes for a
step's ′inputs.files′, so dynamic pipeline generators can compute it too.

Example:

    $ buildkite-agent step hash files "web/**" tsconfig.json --exclude "web/**/*.md"`

type StepHashFilesConfig struct {
	GlobalConfig

	Patterns  []string `cli:"arg:*" label:"file patterns" validate:"required"`
	Exclude   []string `cli:"exclude"`
	Untracked bool     `cli:"untracked"`
}

var StepHashFilesCommand = &cli.Command{
	Name:        "files",
	Usage:       "Print the step reuse digest of committed files matching glob patterns",
	Description: stepHashFilesHelpDescription,
	Flags: slices.Concat(globalFlags(), []cli.Flag{
		&cli.StringSliceFlag{
			Name:  "exclude",
			Usage: "A glob pattern of files to exclude (can be specified multiple times)",
		},
		&cli.BoolFlag{
			Name:  "untracked",
			Usage: "Also hash untracked and ignored files, read from the working tree (default: false)",
		},
	}),
	Action: func(ctx context.Context, c *cli.Command) error {
		ctx, cfg, _, _, done := setupLoggerAndConfig[StepHashFilesConfig](ctx, c)
		defer done()

		res, err := reuse.HashFiles(ctx, ".", cfg.Patterns, cfg.Exclude, cfg.Untracked)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(c.Root().Writer, res.Digest)
		return err
	},
}
