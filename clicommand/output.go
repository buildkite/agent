package clicommand

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/roko"
	"github.com/urfave/cli/v3"
	"go.opentelemetry.io/otel"
)

const outputSetHelpDescription = `Usage:

    buildkite-agent output set <name> <value> [options...]

Description:

Set a named output value of the current step. Steps that use step reuse
declare their output values in ′outputs.values′; when a later build reuses the
step's result, its output values are reused too.

Example:

    $ buildkite-agent output set "bundle_size" "184kB"`

type OutputSetConfig struct {
	GlobalConfig
	APIConfig

	Name  string `cli:"arg:0" label:"output name" validate:"required"`
	Value string `cli:"arg:1" label:"output value" validate:"required"`
	Job   string `cli:"job" validate:"required"`
}

var OutputSetCommand = &cli.Command{
	Name:        "set",
	Usage:       "Set an output value of the current step",
	Description: outputSetHelpDescription,
	Flags: slices.Concat(globalFlags(), apiFlags(), []cli.Flag{
		&cli.StringFlag{
			Name:    "job",
			Value:   "",
			Usage:   "Which job's step should the output value be set on",
			Sources: cli.EnvVars("BUILDKITE_JOB_ID"),
		},
	}),
	Action: func(ctx context.Context, c *cli.Command) error {
		ctx, cfg, l, _, done := setupLoggerAndConfig[OutputSetConfig](ctx, c)
		defer done()
		ctx, span := otel.Tracer("buildkite-agent").Start(ctx, "output-set")
		defer span.End()

		if strings.TrimSpace(cfg.Name) == "" {
			return errors.New("name cannot be empty, or composed of only whitespace characters")
		}

		client := api.NewClient(l, loadAPIClientConfig(cfg, "AgentAccessToken"))
		value := &api.OutputValue{Name: cfg.Name, Value: cfg.Value}

		if err := roko.NewRetrier(
			roko.WithMaxAttempts(10),
			roko.WithStrategy(roko.ExponentialSubsecond(2*time.Second)),
		).DoWithContext(ctx, func(r *roko.Retrier) error {
			resp, err := client.SetOutputValue(ctx, cfg.Job, value)
			if api.BreakOnNonRetryable(r, resp, err) {
				return err
			}
			if err != nil {
				l.Warnf("%s (%s)", err, r)
				return err
			}
			return nil
		}); err != nil {
			return fmt.Errorf("failed to set output value: %w", err)
		}

		return nil
	},
}

const outputGetHelpDescription = `Usage:

    buildkite-agent output get --step <key> <name> [options...]

Description:

Get a named output value of a step in the current build. Exits with status 1
if the value is not set.

Example:

    $ buildkite-agent output get --step "build" "bundle_size"`

type OutputGetConfig struct {
	GlobalConfig
	APIConfig

	Name string `cli:"arg:0" label:"output name" validate:"required"`
	Step string `cli:"step" validate:"required"`
	Job  string `cli:"job" validate:"required"`
}

var OutputGetCommand = &cli.Command{
	Name:        "get",
	Usage:       "Get an output value of a step",
	Description: outputGetHelpDescription,
	Flags: slices.Concat(globalFlags(), apiFlags(), []cli.Flag{
		&cli.StringFlag{
			Name:  "step",
			Value: "",
			Usage: "The key of the step whose output value to get",
		},
		&cli.StringFlag{
			Name:    "job",
			Value:   "",
			Usage:   "Which job's build should the output value be retrieved from",
			Sources: cli.EnvVars("BUILDKITE_JOB_ID"),
		},
	}),
	Action: func(ctx context.Context, c *cli.Command) error {
		ctx, cfg, l, _, done := setupLoggerAndConfig[OutputGetConfig](ctx, c)
		defer done()
		ctx, span := otel.Tracer("buildkite-agent").Start(ctx, "output-get")
		defer span.End()

		client := api.NewClient(l, loadAPIClientConfig(cfg, "AgentAccessToken"))

		r := roko.NewRetrier(
			roko.WithMaxAttempts(10),
			roko.WithStrategy(roko.Constant(5*time.Second)),
		)
		value, resp, err := roko.DoFunc2(ctx, r, func(r *roko.Retrier) (*api.OutputValue, *api.Response, error) {
			value, resp, err := client.GetOutputValue(ctx, cfg.Job, cfg.Step, cfg.Name)
			if api.BreakOnNonRetryable(r, resp, err) {
				return nil, resp, err
			}
			if err != nil {
				l.Warnf("%s (%s)", err, r)
				return nil, resp, err
			}
			return value, resp, nil
		})
		if err != nil {
			if resp != nil && resp.StatusCode == http.StatusNotFound {
				return NewExitError(1, fmt.Errorf("no output value %q is set for step %q", cfg.Name, cfg.Step))
			}
			return fmt.Errorf("failed to get output value: %w", err)
		}

		_, err = fmt.Fprintln(c.Root().Writer, value.Value)
		return err
	},
}
