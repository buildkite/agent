package clicommand

import (
	"context"
	"fmt"
	"io"
	"os"
	"slices"
	"time"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/internal/redact"
	"github.com/buildkite/roko"
	"github.com/urfave/cli"
)

const jobUpdateHelpDescription = `Usage:

    buildkite-agent job update <attribute> <value> [options...]

Description:

Update an attribute of a job. Only command jobs can be
updated, and only before they are finished.

Example:

    $ buildkite-agent job update timeout 20
    $ echo 20 | buildkite-agent job update timeout
`

type JobUpdateConfig struct {
	GlobalConfig
	APIConfig

	Attribute    string   `cli:"arg:0" label:"attribute" validate:"required"`
	Value        string   `cli:"arg:1" label:"value"`
	Job          string   `cli:"job" validate:"required"`
	RedactedVars []string `cli:"redacted-vars" normalize:"list"`
}

var JobUpdateCommand = cli.Command{
	Name:        "update",
	Usage:       "Change the value of an attribute of a job",
	Description: jobUpdateHelpDescription,
	Flags: slices.Concat(globalFlags(), apiFlags(), []cli.Flag{
		cli.StringFlag{
			Name:   "job",
			Value:  "",
			Usage:  "The job to update. Defaults to the current job",
			EnvVar: "BUILDKITE_JOB_ID",
		},
		RedactedVars,
	}),
	Action: func(c *cli.Context) error {
		ctx, cfg, l, _, done := setupLoggerAndConfig[JobUpdateConfig](context.Background(), c)
		defer done()

		if len(c.Args()) < 2 {
			l.Info("Reading value from STDIN")

			input, err := io.ReadAll(os.Stdin)
			if err != nil {
				return fmt.Errorf("failed to read from STDIN: %w", err)
			}
			cfg.Value = string(input)
		}

		client := api.NewClient(l, loadAPIClientConfig(cfg, "AgentAccessToken"))

		needles, _, err := redact.NeedlesFromEnv(cfg.RedactedVars)
		if err != nil {
			return err
		}
		if redactedValue := redact.String(cfg.Value, needles); redactedValue != cfg.Value {
			l.Warn("New value for job %q attribute %q contained one or more secrets from environment variables that have been redacted. If this is deliberate, pass --redacted-vars='' or a list of patterns that does not match the variable containing the secret", cfg.Job, cfg.Attribute)
			cfg.Value = redactedValue
		}

		attrs := map[string]string{cfg.Attribute: cfg.Value}

		if err := roko.NewRetrier(
			roko.WithMaxAttempts(10),
			roko.WithStrategy(roko.ExponentialSubsecond(2*time.Second)),
		).DoWithContext(ctx, func(r *roko.Retrier) error {
			_, resp, err := client.UpdateJob(ctx, cfg.Job, attrs)
			if resp != nil && (resp.StatusCode == 400 || resp.StatusCode == 401 || resp.StatusCode == 404 || resp.StatusCode == 422) {
				r.Break()
			}
			if err != nil {
				l.Warn("%s (%s)", err, r)
				return err
			}
			return nil
		}); err != nil {
			return fmt.Errorf("failed to update job: %w", err)
		}

		return nil
	},
}
