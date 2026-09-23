package clicommand

import (
	"bytes"
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

    buildkite-agent job capture-error <error-code> --message <message> [--context '<json>']

Description:

Reports a structured error on the current job for display in the Buildkite UI
and API. Requires the Local Job API.

The error code is a string identifying the kind of error, such as
image_pull_failed, not necessarily an HTTP status or command exit code.

Optionally include additional details as a JSON object using --context, or use
--context - to read them from standard input.

The complete JSON request and raw context input are each limited to 32 KiB.
The request includes the code, message, context and JSON escaping. Oversized
reports are rejected.

Note: This feature is currently in development and subject to change. It is not
yet available to all customers.`

type JobCaptureErrorConfig struct {
	GlobalConfig
	ErrorCode string `cli:"arg:0" label:"error code"`
	Message   string `cli:"message"`
	Context   string `cli:"context"`
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
		&cli.StringFlag{
			Name: "context", Usage: "Additional error context as a JSON object, or - to read from standard input",
			Sources: cli.EnvVars("BUILDKITE_AGENT_JOB_CAPTURE_ERROR_CONTEXT"),
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
		if cfg.Context != "" {
			var input io.Reader = strings.NewReader(cfg.Context)
			if cfg.Context == "-" {
				input = c.Root().Reader
			}
			data, err := io.ReadAll(io.LimitReader(input, jobapi.MaxCapturedErrorBody+1))
			if err != nil {
				return fmt.Errorf("reading context JSON: %w", err)
			}
			if len(data) > jobapi.MaxCapturedErrorBody {
				return fmt.Errorf("raw context JSON exceeds %d bytes", jobapi.MaxCapturedErrorBody)
			}
			dec := json.NewDecoder(bytes.NewReader(data))
			dec.UseNumber()
			if err := dec.Decode(&capturedError.Context); err != nil {
				return fmt.Errorf("invalid context JSON: %w", err)
			}
			var extra any
			if err := dec.Decode(&extra); !errors.Is(err, io.EOF) || capturedError.Context == nil {
				return errors.New("invalid context JSON: context must contain exactly one object")
			}
		}

		if err := client.CaptureError(ctx, &capturedError); err != nil {
			return fmt.Errorf("failed to capture error through the Local Job API: %w", err)
		}
		return nil
	},
}
