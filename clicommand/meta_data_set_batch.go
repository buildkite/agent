package clicommand

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/internal/redact"
	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/roko"
	"github.com/urfave/cli"
)

const metaDataSetBatchHelpDescription = `Usage:

    buildkite-agent meta-data set-batch <key=value>... [options...]

Description:

Set multiple meta-data key/value pairs on a build in a single request.

Each argument must be in key=value format.

Keys and values must be non-empty strings, and strings containing only
whitespace characters are not allowed.

Example:

    $ buildkite-agent meta-data set-batch foo=bar "greeting=hello world"
    $ buildkite-agent meta-data set-batch duration:spec/a.rb=2.341 duration:spec/b.rb=5.672`

type MetaDataSetBatchConfig struct {
	GlobalConfig
	APIConfig

	Job          string   `cli:"job" validate:"required"`
	RedactedVars []string `cli:"redacted-vars" normalize:"list"`
}

var MetaDataSetBatchCommand = cli.Command{
	Name:        "set-batch",
	Usage:       "Set multiple meta-data key/value pairs on a build",
	Description: metaDataSetBatchHelpDescription,
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
		ctx, cfg, l, _, done := setupLoggerAndConfig[MetaDataSetBatchConfig](ctx, c)
		defer done()

		args := c.Args()
		if len(args) == 0 {
			return errors.New("at least one key=value argument is required")
		}

		items, err := parseMetaDataBatchArgs(args)
		if err != nil {
			return err
		}

		needles, _, err := redact.NeedlesFromEnv(cfg.RedactedVars)
		if err != nil {
			return err
		}

		for i := range items {
			if redactedValue := redact.String(items[i].Value, needles); redactedValue != items[i].Value {
				l.Warn("Meta-data value for key %q contained one or more secrets from environment variables that have been redacted. If this is deliberate, pass --redacted-vars='' or a list of patterns that does not match the variable containing the secret", items[i].Key)
				items[i].Value = redactedValue
			}
		}

		return setMetaDataBatch(ctx, cfg, l, items)
	},
}

func parseMetaDataBatchArgs(args []string) ([]api.MetaData, error) {
	items := make([]api.MetaData, 0, len(args))
	for _, arg := range args {
		key, value, ok := strings.Cut(arg, "=")
		if !ok {
			return nil, fmt.Errorf("invalid argument %q: must be in key=value format", arg)
		}
		if strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("invalid argument %q: key cannot be empty, or composed of only whitespace characters", arg)
		}
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("invalid argument %q: value cannot be empty, or composed of only whitespace characters", arg)
		}
		items = append(items, api.MetaData{Key: key, Value: value})
	}
	return items, nil
}

func setMetaDataBatch(ctx context.Context, cfg MetaDataSetBatchConfig, l logger.Logger, items []api.MetaData) error {
	client := api.NewClient(l, loadAPIClientConfig(cfg, "AgentAccessToken"))

	batch := &api.MetaDataBatch{Items: items}

	if err := roko.NewRetrier(
		roko.WithMaxAttempts(10),
		roko.WithStrategy(roko.ExponentialSubsecond(2*time.Second)),
	).DoWithContext(ctx, func(r *roko.Retrier) error {
		resp, err := client.SetMetaDataBatch(ctx, cfg.Job, batch)
		if resp != nil && (resp.StatusCode == 401 || resp.StatusCode == 404) {
			r.Break()
		}
		if err != nil {
			l.Warn("%s (%s)", err, r)
			return err
		}
		return nil
	}); err != nil {
		return fmt.Errorf("failed to set meta-data batch: %w", err)
	}

	return nil
}
