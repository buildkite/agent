package clicommand

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/internal/redact"
	"github.com/buildkite/roko"
	"github.com/urfave/cli"
)

const metaDataSetHelpDescription = `Usage:

    buildkite-agent meta-data set <key> [value] [options...]

Description:

Set arbitrary data on a build using a basic key/value store.

You can supply the value as an argument to the command, or pipe in a file or
script output.

The value must be a non-empty string, and strings containing only whitespace
characters are not allowed.

Example:

    $ buildkite-agent meta-data set "foo" "bar"
    $ buildkite-agent meta-data set "foo" < ./tmp/meta-data-value
    $ ./script/meta-data-generator | buildkite-agent meta-data set "foo"`

type MetaDataSetConfig struct {
	GlobalConfig
	APIConfig

	Key          string   `cli:"arg:0" label:"meta-data key" validate:"required"`
	Value        string   `cli:"arg:1" label:"meta-data value"`
	Job          string   `cli:"job" validate:"required"`
	RedactedVars []string `cli:"redacted-vars" normalize:"list"`
}

var MetaDataSetCommand = cli.Command{
	Name:        "set",
	Usage:       "Set data on a build",
	Description: metaDataSetHelpDescription,
	Flags: slices.Concat(globalFlags(), apiFlags(), []cli.Flag{
		cli.StringFlag{
			Name:   "job",
			Value:  "",
			Usage:  "Which job's build should the meta-data be set on",
			EnvVar: "BUILDKITE_JOB_ID",
		},
		RedactedVars,
	}),
	Action: func(c *cli.Context) error {
		ctx := context.Background()
		ctx, cfg, l, _, done := setupLoggerAndConfig[MetaDataSetConfig](ctx, c)
		defer done()

		// Read the value from STDIN if argument omitted entirely
		if len(c.Args()) < 2 {
			l.Info("Reading meta-data value from STDIN")

			input, err := io.ReadAll(os.Stdin)
			if err != nil {
				return fmt.Errorf("failed to read from STDIN: %w", err)
			}
			cfg.Value = string(input)
		}

		if strings.TrimSpace(cfg.Key) == "" {
			return errors.New("key cannot be empty, or composed of only whitespace characters")
		}

		if strings.TrimSpace(cfg.Value) == "" {
			return errors.New("value cannot be empty, or composed of only whitespace characters")
		}

		// Apply secret redaction to the value.
		needles, _, err := redact.NeedlesFromEnv(cfg.RedactedVars)
		if err != nil {
			return err
		}
		if redactedValue := redact.String(cfg.Value, needles); redactedValue != cfg.Value {
			l.Warn("Meta-data value for key %q contained one or more secrets from environment variables that have been redacted. If this is deliberate, pass --redacted-vars='' or a list of patterns that does not match the variable containing the secret", cfg.Key)
			cfg.Value = redactedValue
		}

		// Create the API client
		client := api.NewClient(l, loadAPIClientConfig(cfg, "AgentAccessToken"))

		// Create the meta data to set
		metaData := &api.MetaData{
			Key:   cfg.Key,
			Value: cfg.Value,
		}

		// Set the meta data
		if err := roko.NewRetrier(
			// 10x2 sec -> 2, 3, 5, 8, 13, 21, 34, 55, 89 seconds (total delay: 233 seconds)
			roko.WithMaxAttempts(10),
			roko.WithStrategy(roko.ExponentialSubsecond(2*time.Second)),
		).DoWithContext(ctx, func(r *roko.Retrier) error {
			resp, err := client.SetMetaData(ctx, cfg.Job, metaData)
			if resp != nil && (resp.StatusCode == 401 || resp.StatusCode == 404) {
				r.Break()
			}
			if err != nil {
				l.Warn("%s (%s)", err, r)
				return err
			}
			return nil
		}); err != nil {
			return fmt.Errorf("failed to set meta-data: %w", err)
		}

		return nil
	},
}
