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

    buildkite-agent job capture-error <code> --message <message> [--context '<json>']

Description:

Send a structured error to the running parent agent through the authenticated
Local Job API. The parent validates and normalizes the error. There is no
direct network fallback when the Local Job API is unavailable.

Note: This feature is currently in development and subject to change. It is not
yet available to all customers.`

type JobCaptureErrorConfig struct {
	GlobalConfig
	Code    string `cli:"arg:0" label:"error code"`
	Message string `cli:"message"`
	Context string `cli:"context"`
}

var JobCaptureErrorCommand = &cli.Command{
	Name:        "capture-error",
	Usage:       "Capture a structured error for the current job (experimental)",
	Hidden:      true,
	Description: jobCaptureErrorHelpDescription,
	Flags: append(globalFlags(),
		&cli.StringFlag{Name: "message", Usage: "A human-readable description of the error", Required: true},
		&cli.StringFlag{Name: "context", Usage: "Additional error context as a JSON object"},
	),
	Action: func(ctx context.Context, c *cli.Command) error {
		ctx, cfg, _, _, done := setupLoggerAndConfig[JobCaptureErrorConfig](ctx, c)
		defer done()

		if c.Args().Len() != 1 || strings.TrimSpace(cfg.Code) == "" {
			return errors.New("exactly one error code is required")
		}
		if strings.TrimSpace(cfg.Message) == "" {
			return errors.New("message must not be blank")
		}
		capturedError := jobapi.CapturedError{Code: cfg.Code, Message: cfg.Message}
		if c.IsSet("context") {
			dec := json.NewDecoder(strings.NewReader(cfg.Context))
			dec.UseNumber()
			if err := dec.Decode(&capturedError.Context); err != nil {
				return fmt.Errorf("invalid context JSON: %w", err)
			}
			var extra any
			if err := dec.Decode(&extra); !errors.Is(err, io.EOF) || capturedError.Context == nil {
				return errors.New("invalid context JSON: context must contain exactly one object")
			}
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
