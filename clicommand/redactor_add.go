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
// and update the usage string in LogRedactCommand
const (
	FormatStringJSON = "json"
	FormatStringNone = "none"
	// TODO: we should parse .env files
	// TODO: we should parse ssh private keys. The format is in https://datatracker.ietf.org/doc/html/rfc7468
)

var (
	secretsFormats = []string{FormatStringJSON, FormatStringNone}

	errSecretParse   = errors.New("failed to parse secrets")
	errSecretRedact  = errors.New("failed to redact secrets")
	errOIDCRedact    = errors.New("failed to redact OIDC token")
	errUnknownFormat = errors.New("unknown format")
)

type RedactorAddConfig struct {
	GlobalConfig
	APIConfig

	File   string `cli:"arg:0"`
	Format string `cli:"format"`
}

var RedactorAddCommand = cli.Command{
	Name:  "add",
	Usage: "Add values to redact from a job's log output",
	Description: `Usage:

    buildkite-agent redactor add [options...] [file-with-content-to-redact]

Description:

This command may be used to parse a file for values to redact from a
running job's log output. If you dynamically fetch secrets during a job,
it is recommended that you use this command to ensure they will be
redacted from subsequent logs. Secrets fetched with the builtin
′secret get′ command do not require the use of this command, they will
be redacted automatically.

Examples:

To redact the verbatim contents of the file 'id_ed25519' from future logs:

    $ buildkite-agent redactor add id_ed25519

To redact the string 'llamasecret' from future logs:

    $ echo llamasecret | buildkite-agent redactor add

Pass a flat JSON object whose keys are unique and whose values are your secrets:

    $ echo '{"db_password":"secret1","api_token":"secret2","ssh_key":"secret3"}' | buildkite-agent redactor add --format json

Or

    $ buildkite-agent redactor add --format json my-secrets.json

JSON does not allow duplicate keys. If you repeat the same key ("key"), the JSON parser keeps only the final entry, so only that single value is added to the redactor:

    $ echo '{"key":"value1","key":"value2","key":"value3"}' | buildkite-agent redactor add --format json`,
	Flags: slices.Concat(globalFlags(), apiFlags(), []cli.Flag{
		cli.StringFlag{
			Name:   "format",
			Usage:  "The format for the input, whose value is either ′json′ or ′none′. ′none′ adds the entire input's content to the redactor, with the exception of leading and trailing space. ′json′ parses the input's content as a JSON object, where each value of each key is added to the redactor.",
			EnvVar: "BUILDKITE_AGENT_REDACT_ADD_FORMAT",
			Value:  FormatStringNone,
		},
	}),
	Action: func(c *cli.Context) error {
		ctx := context.Background()
		ctx, cfg, l, _, done := setupLoggerAndConfig[RedactorAddConfig](ctx, c)
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
			defer secretsFile.Close() //nolint:errcheck // read-only file; close error is inconsequential

			secretsReader = bufio.NewReader(secretsFile)
		}

		l.Info("Reading secrets from %s for redaction", fileName)

		secrets, err := ParseSecrets(l, cfg, secretsReader)
		if err != nil {
			if cfg.Debug {
				return err
			}
			return errSecretParse
		}

		client, err := jobapi.NewDefaultClient(ctx)
		if err != nil {
			return fmt.Errorf("failed to create Job API client: %w", err)
		}

		if err := AddToRedactor(ctx, l, client, secrets...); err != nil {
			if cfg.Debug {
				return err
			}
			return errSecretRedact
		}

		return nil
	},
}

func ParseSecrets(
	l logger.Logger,
	cfg RedactorAddConfig,
	secretsReader io.Reader,
) ([]string, error) {
	switch cfg.Format {
	case FormatStringJSON:
		secrets := &map[string]string{}
		if err := json.NewDecoder(secretsReader).Decode(&secrets); err != nil {
			return nil, fmt.Errorf("failed to parse as string valued JSON: %w", err)
		}

		parsedSecrets := make([]string, 0, len(*secrets))
		for _, secret := range *secrets {
			parsedSecrets = append(parsedSecrets, secret)
		}

		return parsedSecrets, nil

	case FormatStringNone:
		readSecret, err := io.ReadAll(secretsReader)
		if err != nil {
			return nil, fmt.Errorf("failed to read secret: %w", err)
		}

		return []string{strings.TrimSpace(string(readSecret))}, nil

	default:
		return nil, fmt.Errorf("%s: %w", cfg.Format, errUnknownFormat)
	}
}

func AddToRedactor(
	ctx context.Context,
	l logger.Logger,
	client *jobapi.Client,
	secrets ...string,
) error {
	for _, secret := range secrets {
		if _, err := client.RedactionCreate(ctx, secret); err != nil {
			return fmt.Errorf("failed to add secret to the redactor: %w", err)
		}
	}
	return nil
}
