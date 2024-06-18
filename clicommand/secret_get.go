package clicommand

import (
	"context"
	"fmt"
	"runtime"
	"strings"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/jobapi"
	"github.com/urfave/cli"
)

type SecretGetConfig struct {
	Key                    string `cli:"arg:0"`
	Job                    string `cli:"job" validate:"required"`
	SkipRedaction          bool   `cli:"skip-redaction"`
	NoNormalizeLineEndings bool   `cli:"no-normalize-line-endings"`

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

var SecretGetCommand = cli.Command{
	Name:  "get",
	Usage: "Get a secret by its key and print it to stdout",
	Description: `Usage:

    buildkite-agent secret get [key] [options...]

Description:

Gets a secret from Buildkite secrets and prints it to stdout. The ′key′
specified in this command is the key's name defined for the secret in its
cluster. The key's name is case insensitive in this command, and the
key's value is automatically redacted in the build logs.

The secret's value is redacted in the build logs by default. If you want to
print the secret to the build logs, use the --skip-redaction flag.

By default, the secret's line endings are normalized to whatever platform the
agent is running on - CRLF for windows, and LF for other platforms. If you want
to preserve the line endings in the secret, use the --no-normalize-line-endings flag.

Examples:

The following examples reference the same Buildkite secret ′key′:

    $ buildkite-agent secret get deploy_key
		$ buildkite-agent secret get DEPLOY_KEY`,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:   "job",
			Usage:  "Which job should should the secret be for",
			EnvVar: "BUILDKITE_JOB_ID",
		},
		cli.BoolFlag{
			Name:   "skip-redaction",
			Usage:  "Skip redacting the retrieved secret from the logs. Then, the command will print the secret to the Job's logs if called directly.",
			EnvVar: "BUILDKITE_AGENT_SECRET_GET_SKIP_SECRET_REDACTION",
		},
		cli.BoolFlag{
			Name:   "no-normalize-line-endings",
			Usage:  "Don't normalize line endings in the secret value",
			EnvVar: "BUILDKITE_AGENT_SECRET_GET_NO_NORMALIZE_LINE_ENDINGS",
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
		ctx, cfg, l, _, done := setupLoggerAndConfig[SecretGetConfig](ctx, c)
		defer done()

		agentClient := api.NewClient(l, loadAPIClientConfig(cfg, "AgentAccessToken"))
		secret, _, err := agentClient.GetSecret(ctx, &api.GetSecretRequest{Key: cfg.Key, JobID: cfg.Job})
		if err != nil {
			return err
		}

		val := secret.Value
		if !cfg.NoNormalizeLineEndings {
			val = normalizeLineEndings(runtime.GOOS, val)
		}

		jobClient, err := jobapi.NewDefaultClient(ctx)
		if err != nil {
			return fmt.Errorf("failed to create Job API client: %w", err)
		}

		if !cfg.SkipRedaction {
			toRedact := []string{val}
			if val != secret.Value { // ie, we have normalised the line endings
				toRedact = append(toRedact, secret.Value) // redact the original value as well, just in case
			}

			if err := AddToRedactor(ctx, l, jobClient, toRedact...); err != nil {
				if cfg.Debug {
					return err
				}
				return errSecretRedact
			}
		}

		_, err = fmt.Fprintln(c.App.Writer, val)

		return err
	},
}

func normalizeLineEndings(platform, s string) string {
	switch platform {
	case "windows":
		b := strings.Builder{}
		for idx, c := range s {
			// replace \n with \r\n, but only if the \n is not already preceded by \r
			if c == '\n' && (idx == 0 || s[idx-1] != '\r') {
				b.WriteString("\r\n")
				continue
			}

			b.WriteRune(c)
		}

		return b.String()
	default:
		return strings.ReplaceAll(s, "\r\n", "\n")
	}
}
