package clicommand

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/buildkite/agent/v4/jobapi"
	"github.com/urfave/cli/v3"
)

const jobCaptureErrorHelpDescription = `Usage:

    buildkite-agent job capture-error '<json>'

Description:

Send a structured error to the running parent agent through the authenticated
Local Job API. The parent validates and normalizes the error. There is no
direct network fallback when the Local Job API is unavailable.

Note: This feature is currently in development and subject to change. It is not
yet available to all customers.`

type JobCaptureErrorConfig struct {
	GlobalConfig
	Payload string `cli:"arg:0" label:"JSON payload" validate:"required"`
}

var JobCaptureErrorCommand = &cli.Command{
	Name:        "capture-error",
	Usage:       "Capture a structured error for the current job (experimental)",
	Hidden:      true,
	Description: jobCaptureErrorHelpDescription,
	Flags:       globalFlags(),
	Action: func(ctx context.Context, c *cli.Command) error {
		ctx, cfg, _, _, done := setupLoggerAndConfig[JobCaptureErrorConfig](ctx, c)
		defer done()

		var capturedError jobapi.CapturedError
		dec := json.NewDecoder(strings.NewReader(cfg.Payload))
		dec.DisallowUnknownFields()
		dec.UseNumber()
		if err := dec.Decode(&capturedError); err != nil {
			return fmt.Errorf("invalid captured-error JSON: %w", err)
		}
		var extra any
		if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
			return errors.New("invalid captured-error JSON: payload must contain exactly one object")
		}

		client, err := jobapi.NewDefaultClient(ctx)
		if err != nil {
			return fmt.Errorf("the Local Job API is required to safely capture diagnostics: %w", err)
		}
		if err := client.CaptureError(ctx, &capturedError); err != nil {
			return fmt.Errorf("failed to capture error through the Local Job API: %w", err)
		}
		return nil
	},
}
