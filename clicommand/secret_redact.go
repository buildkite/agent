package clicommand

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/buildkite/agent/v3/jobapi"
	"github.com/buildkite/agent/v3/logger"
	"github.com/urfave/cli"
)

// Note: if you add a new format string, make sure to add it to `secretsFormats`
// and update the usage string in SecretRedactCommand
const (
	formatStringJSON = "json"
	formatStringNone = "none"
	// TODO: we should have a an `env` format that parses .env files
)

var (
	secretsFormats = []string{formatStringJSON, formatStringNone}

	errSecretParseOrRedact = errors.New("failed to parse or redact secret")
	errUnknownFormat       = errors.New("unknown format")
)

type SecretRedactConfig struct {
	File   string `cli:"arg:0"`
	Format string `cli:"format"`

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
		cli.StringFlag{
			Name: "format",
			Usage: fmt.Sprintf(
				"The format for the input, one of: %s. ′none′ will add the entire input as a to the redactor, save for leading and trailing whitespace, ′json′ will parse it a string valued JSON Object, where each value of each key will be added to the redactor.",
				secretsFormats,
			),
			EnvVar: "BUILDKITE_AGENT_SECRET_REDACT_ENV_FORMAT",
			Value:  formatStringNone,
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

		if !slices.Contains(secretsFormats, cfg.Format) {
			return fmt.Errorf("invalid format: %s, must be one of %q", cfg.Format, secretsFormats)
		}

		fileName := "(stdin)"
		// TODO: replace os.Stdin with c.App.Reader in cli v2+
		secretsReader := bufio.NewReader(os.Stdin)
		if cfg.File != "" {
			fileName = cfg.File

			secretsFile, err := os.Open(fileName)
			if err != nil {
				return fmt.Errorf("failed to open file %s: %w", fileName, err)
			}
			defer secretsFile.Close()

			secretsReader = bufio.NewReader(secretsFile)
		}

		l.Info("Reading secrets from %s for redaction", fileName)

		client, err := jobapi.NewDefaultClient(ctx)
		if err != nil {
			return fmt.Errorf("failed to create Job API client: %w", err)
		}

		if err := ParseAndRedactSecret(ctx, l, cfg, client, secretsReader); err != nil {
			if cfg.Debug {
				return err
			}
			return errSecretParseOrRedact
		}

		return nil
	},
}

func ParseAndRedactSecret(
	ctx context.Context,
	l logger.Logger,
	cfg SecretRedactConfig,
	client *jobapi.Client,
	secretsReader io.Reader,
) error {
	switch cfg.Format {
	case formatStringJSON:
		secrets := &map[string]string{}
		if err := json.NewDecoder(secretsReader).Decode(&secrets); err != nil {
			return fmt.Errorf("failed to parse as string valued JSON: %w", err)
		}

		for _, secret := range *secrets {
			if _, err := client.RedactionCreate(ctx, secret); err != nil {
				return fmt.Errorf("failed to add secret to the redactor: %w", err)
			}
		}

		return nil

	case formatStringNone:
		readSecret, err := io.ReadAll(secretsReader)
		if err != nil {
			return fmt.Errorf("failed to read secret: %w", err)
		}

		toRedact := strings.TrimSpace(string(readSecret))
		if _, err := client.RedactionCreate(ctx, toRedact); err != nil {
			return fmt.Errorf("failed to add secret to the redactor: %w", err)
		}

		return nil

	default:
		return fmt.Errorf("%s: %w", cfg.Format, errUnknownFormat)
	}
}
