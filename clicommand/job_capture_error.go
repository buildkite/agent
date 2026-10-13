package clicommand

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/buildkite/agent/v4/jobapi"
	"github.com/urfave/cli/v3"
)

const jobCaptureErrorHelpDescription = `Usage:

    buildkite-agent job capture-error <error-code> --message <message>

Description:

Reports a structured error on the current job for display in the Buildkite UI
and API. Requires the Local Job API.

The error code is a string identifying the kind of error, such as
image_pull_failed, not necessarily an HTTP status or command exit code.

Include all important details in the message.

Values registered for job-log redaction are redacted from the code and message
before the report is sent to Buildkite. As in job logs, values that look like
Buildkite-issued tokens are also redacted. Register dynamically obtained secrets
with buildkite-agent redactor add before reporting them. This does not
automatically detect other sensitive information.

Reports are rejected if redaction causes an invalid error code or an oversized
payload.

This command limits error reports to 32 KiB, including JSON encoding.
If a report is too large, shorten the message.

Note: This feature is currently in development and subject to change. It is not
yet available to all customers.`

type JobCaptureErrorConfig struct {
	GlobalConfig
	ErrorCode string `cli:"arg:0" label:"error code"`
	Message   string `cli:"message"`
}

var JobCaptureErrorCommand = &cli.Command{
	Name:        "capture-error",
	Usage:       "Capture a structured error for the current job (experimental)",
	Hidden:      true,
	Description: jobCaptureErrorHelpDescription,
	Flags: append(globalFlags(),
		&cli.StringFlag{
			Name: "message", Usage: "A human-readable description of the error", Required: true,
			Sources: cli.EnvVars("BUILDKITE_AGENT_JOB_CAPTURE_ERROR_MESSAGE"),
		},
	),
	Action: func(ctx context.Context, c *cli.Command) error {
		ctx, cfg, _, _, done := setupLoggerAndConfig[JobCaptureErrorConfig](ctx, c)
		defer done()

		if c.Args().Len() != 1 || strings.TrimSpace(cfg.ErrorCode) == "" {
			return errors.New("exactly one error code is required")
		}
		if strings.TrimSpace(cfg.Message) == "" {
			return errors.New("message must not be blank")
		}
		client, err := jobapi.NewDefaultClient(ctx)
		if err != nil {
			return fmt.Errorf("the Local Job API is required to safely capture diagnostics: %w", err)
		}

		capturedError := jobapi.CapturedError{Code: cfg.ErrorCode, Message: cfg.Message}
		if err := client.CaptureError(ctx, &capturedError); err != nil {
			return fmt.Errorf("failed to capture error through the Local Job API: %w", err)
		}
		return nil
	},
}
