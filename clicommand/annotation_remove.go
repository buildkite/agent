package clicommand

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/roko"
	"github.com/urfave/cli"
)

const annotationRemoveHelpDescription = `Usage:

    buildkite-agent annotation remove [arguments...]

Description:

Remove an existing annotation which was previously published using the
buildkite-agent annotate command.

If you leave context blank, it will use the default context.

Example:

    $ buildkite-agent annotation remove
    $ buildkite-agent annotation remove --context "remove-me"`

type AnnotationRemoveConfig struct {
	GlobalConfig
	APIConfig

	Context string `cli:"context" validate:"required"`
	Scope   string `cli:"scope"`
	Job     string `cli:"job" validate:"required"`
}

var AnnotationRemoveCommand = cli.Command{
	Name:        "remove",
	Usage:       "Remove an existing annotation from a Buildkite build",
	Description: annotationRemoveHelpDescription,
	Flags: slices.Concat(globalFlags(), apiFlags(), []cli.Flag{
		cli.StringFlag{
			Name:   "context",
			Value:  "default",
			Usage:  "The context of the annotation used to differentiate this annotation from others",
			EnvVar: "BUILDKITE_ANNOTATION_CONTEXT",
		},
		cli.StringFlag{
			Name:   "scope",
			Value:  "build",
			Usage:  "The scope of the annotation to remove. One of either 'build' or 'job'",
			EnvVar: "BUILDKITE_ANNOTATION_SCOPE",
		},
		cli.StringFlag{
			Name:   "job",
			Value:  "",
			Usage:  "Which job is removing the annotation",
			EnvVar: "BUILDKITE_JOB_ID",
		},
	}),
	Action: func(c *cli.Context) error {
		ctx := context.Background()
		ctx, cfg, l, _, done := setupLoggerAndConfig[AnnotationRemoveConfig](ctx, c)
		defer done()

		// Create the API client
		client := api.NewClient(l, loadAPIClientConfig(cfg, "AgentAccessToken"))

		// Retry the removal a few times before giving up
		if err := roko.NewRetrier(
			roko.WithMaxAttempts(5),
			roko.WithStrategy(roko.Constant(1*time.Second)),
			roko.WithJitter(),
		).DoWithContext(ctx, func(r *roko.Retrier) error {
			// Attempt to remove the annotation
			resp, err := client.AnnotationRemove(ctx, cfg.Job, cfg.Context, cfg.Scope)

			// Don't bother retrying if the response was one of these statuses
			if resp != nil && (resp.StatusCode == 401 || resp.StatusCode == 404 || resp.StatusCode == 400 || resp.StatusCode == 410) {
				r.Break()
				return err
			}

			// Show the unexpected error
			if err != nil {
				l.Warn("%s (%s)", err, r)
				return err
			}
			return nil
		}); err != nil {
			return fmt.Errorf("failed to remove annotation: %w", err)
		}

		l.Debug("Successfully removed annotation")

		return nil
	},
}
