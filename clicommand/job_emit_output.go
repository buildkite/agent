package clicommand

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"

	"github.com/buildkite/agent/v4/api"
	"github.com/urfave/cli/v3"
)

const jobEmitOutputHelpDescription = `Usage:

    buildkite-agent job emit-output --schema <schema> --payload <json> [options...]

Description:

Emit a structured output for the current job. The payload may be any valid JSON
value. Available schemas and payload validation are managed by Buildkite.

Example:

    $ buildkite-agent job emit-output --schema="example.error.v1" --payload='{"code":"rate_limited","retryable":true}'
`

type JobEmitOutputConfig struct {
	GlobalConfig
	APIConfig

	Schema  string `cli:"schema" validate:"required"`
	Payload string `cli:"payload" validate:"required"`
}

var JobEmitOutputCommand = &cli.Command{
	Name:        "emit-output",
	Usage:       "Emit a structured output for the current job",
	Description: jobEmitOutputHelpDescription,
	Flags: slices.Concat(globalFlags(), apiFlags(), []cli.Flag{
		&cli.StringFlag{
			Name:  "schema",
			Usage: "The schema that describes the output payload",
		},
		&cli.StringFlag{
			Name:  "payload",
			Usage: "The output payload as a JSON value",
		},
	}),
	Action: func(ctx context.Context, c *cli.Command) error {
		ctx, cfg, l, _, done := setupLoggerAndConfig[JobEmitOutputConfig](ctx, c)
		defer done()

		var payload json.RawMessage
		if err := json.Unmarshal([]byte(cfg.Payload), &payload); err != nil {
			return fmt.Errorf("invalid JSON for --payload: %w", err)
		}

		jobID := os.Getenv("BUILDKITE_JOB_ID")
		if jobID == "" {
			return fmt.Errorf("BUILDKITE_JOB_ID is not set: this command must be run from within a job")
		}

		client := api.NewClient(l, loadAPIClientConfig(cfg, "AgentAccessToken"))
		// Creating an output is not idempotent, and the API has no idempotency
		// key, so a retry after an ambiguous response could create a duplicate.
		output, _, err := client.CreateJobOutput(ctx, jobID, &api.JobOutputRequest{
			Schema:  cfg.Schema,
			Payload: payload,
		})
		if err != nil {
			return fmt.Errorf("failed to emit job output: %w", err)
		}

		l.Debugf("Successfully emitted job output %s", output.UUID)
		return nil
	},
}
