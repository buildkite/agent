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

const stepGetHelpDescription = `Usage:

    buildkite-agent step get <attribute> [options...]

Description:

Retrieve the value of an attribute in a step. If no attribute is passed, the
entire step will be returned.

In the event a complex object is returned (an object or an array),
you'll need to supply the --format option to tell the agent how it should
output the data (currently only JSON is supported).

Example:

    $ buildkite-agent step get "label" --step "key"
    $ buildkite-agent step get --format json
    $ buildkite-agent step get "state" --step "my-other-step"`

type StepGetConfig struct {
	GlobalConfig
	APIConfig

	Attribute string `cli:"arg:0" label:"step attribute"`
	StepOrKey string `cli:"step" validate:"required"`
	Build     string `cli:"build"`
	Format    string `cli:"format"`
}

var StepGetCommand = cli.Command{
	Name:        "get",
	Usage:       "Get the value of an attribute",
	Description: stepGetHelpDescription,
	Flags: slices.Concat(globalFlags(), apiFlags(), []cli.Flag{
		cli.StringFlag{
			Name:   "step",
			Value:  "",
			Usage:  "The step to get. Can be either its ID (BUILDKITE_STEP_ID) or key (BUILDKITE_STEP_KEY)",
			EnvVar: "BUILDKITE_STEP_ID",
		},
		cli.StringFlag{
			Name:   "build",
			Value:  "",
			Usage:  "The build to look for the step in. Only required when targeting a step using its key (BUILDKITE_STEP_KEY)",
			EnvVar: "BUILDKITE_BUILD_ID",
		},
		cli.StringFlag{
			Name:   "format",
			Value:  "",
			Usage:  "The format to output the attribute value in (currently only JSON is supported)",
			EnvVar: "BUILDKITE_STEP_GET_FORMAT",
		},
	}),
	Action: func(c *cli.Context) error {
		ctx, cfg, l, _, done := setupLoggerAndConfig[StepGetConfig](context.Background(), c)
		defer done()

		// Create the API client
		client := api.NewClient(l, loadAPIClientConfig(cfg, "AgentAccessToken"))

		// Create the request
		stepExportRequest := &api.StepExportRequest{
			Build:     cfg.Build,
			Attribute: cfg.Attribute,
			Format:    cfg.Format,
		}

		// Find the step attribute
		r := roko.NewRetrier(
			roko.WithMaxAttempts(10),
			roko.WithStrategy(roko.Constant(5*time.Second)),
		)
		stepExportResponse, err := roko.DoFunc(ctx, r, func(r *roko.Retrier) (*api.StepExportResponse, error) {
			stepExportResponse, resp, err := client.StepExport(ctx, cfg.StepOrKey, stepExportRequest)
			// Don't bother retrying if the response was one of these statuses
			if resp != nil && (resp.StatusCode == 401 || resp.StatusCode == 404 || resp.StatusCode == 400) {
				r.Break()
			}
			if err != nil {
				l.Warn("%s (%s)", err, r)
			}
			return stepExportResponse, err
		})
		if err != nil {
			return fmt.Errorf("failed to get step: %w", err)
		}

		// Output the value to STDOUT
		_, err = fmt.Fprintln(c.App.Writer, stepExportResponse.Output)
		return err
	},
}
