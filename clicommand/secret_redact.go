package clicommand

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"

	"github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/jobapi"
	"github.com/buildkite/agent/v3/logger"
	"github.com/urfave/cli"
)

type SecretRedactConfig struct {
	File      string `cli:"arg:0"`
	EnvFormat bool   `cli:"env-format"`

	// Global flags
	Debug       bool     `cli:"debug"`
	LogLevel    string   `cli:"log-level"`
	NoColor     bool     `cli:"no-color"`
	Experiments []string `cli:"experiment" normalize:"list"`
	Profile     string   `cli:"profile"`

	// API config
	DebugHTTP        bool   `cli:"debug-http"`
	AgentAccessToken string `cli:"agent-access-token" validate:"required"`
	Endpoint         string `cli:"endpoint" validate:"required"`
	NoHTTP2          bool   `cli:"no-http2"`
}

var SecretRedactCommand = cli.Command{
	Name:        "redact",
	Usage:       "Add to the agent's list of redacted strings in log output",
	Description: "Add lines in a file to the agent's list of redacted strings in log output",
	Flags: []cli.Flag{
		cli.BoolFlag{
			Name:   "env-format",
			Usage:  "Use ′key=secret′ format to parse the file. The ′key′ part will not be redacted.",
			EnvVar: "BUILDKITE_AGENT_SECRET_REDACT_ENV_FORMAT",
		},

		// API Flags
		AgentAccessTokenFlag,
		EndpointFlag,
		NoHTTP2Flag,
		DebugHTTPFlag,

		// Global flags
		NoColorFlag,
		DebugFlag,
		LogLevelFlag,
		ExperimentsFlag,
		ProfileFlag,
	},
	Action: func(c *cli.Context) error {
		ctx := context.Background()
		ctx, cfg, l, _, done := setupLoggerAndConfig[SecretRedactConfig](ctx, c)
		defer done()

		// TODO: replace os.Stdin with c.App.Reader in cli v2+
		return addSecretsToRedactor(ctx, l, cfg, os.Stdin)
	},
}

func addSecretsToRedactor(
	ctx context.Context,
	l logger.Logger,
	cfg SecretRedactConfig,
	stdin io.Reader,
) error {
	fileName := "(stdin)"
	secretsReader := stdin
	if cfg.File != "" {
		fileName = cfg.File

		secretsFile, err := os.Open(fileName)
		if err != nil {
			return fmt.Errorf("failed to open file %s: %w", fileName, err)
		}
		defer secretsFile.Close()

		secretsReader = secretsFile
	}

	l.Info("Reading secrets from %s for redaction", fileName)

	client, err := jobapi.NewDefaultClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to create Job API client: %w", err)
	}

	scanner := bufio.NewScanner(secretsReader)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		secret := line
		if cfg.EnvFormat {
			var ok bool
			_, secret, ok = env.Split(line)
			if !ok {
				lineLogger := l
				if cfg.Debug {
					lineLogger = l.WithFields(logger.StringField("line", line))
				}
				lineLogger.Warn("Failed to parse line as key=value format, skipping.")
			}
		}

		if _, err := client.RedactionCreate(ctx, secret); err != nil {
			if cfg.Debug {
				return fmt.Errorf("failed to add secret to the redactor: %w", err)
			} else {
				return fmt.Errorf("failed to add a secret to the redactor")
			}
		}
	}

	return nil
}
