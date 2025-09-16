package clicommand

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/internal/secrets"
	"github.com/buildkite/agent/v3/jobapi"
	"github.com/urfave/cli"
)

type SecretBatchReadConfig struct {
	GlobalConfig
	APIConfig

	Keys          []string `cli:"arg:*"`
	Format        string   `cli:"format" validate:"required"`
	Job           string   `cli:"job" validate:"required"`
	SkipRedaction bool     `cli:"skip-redaction"`
}

var SecretBatchReadCommand = cli.Command{
	Name:  "batch-read",
	Usage: "Concurrently fetch a batch of secrets by their keys",
	Description: `Usage:

    buildkite-agent secret batch-read [options...] [key1] [key2] ...

Description:

Concurrently fetches a batch of secrets from Buildkite secrets and prints them to stdout as key-value pairs,
either in JSON (default) or env format. Keys are case-insensitive, and secret values will be automatically redacted
from the build logs unless the ′skip-redaction′ flag is used.

If any request for a secret fails, the command will return a non-zero exit code and print details of all failed secrets
(and won't print any secrets).

Examples:

    $ buildkite-agent secret batch-read deploy_key github_api_token something_else_thats_secret
    {"deploy_key": "...", "github_api_token": "...", "something_else_thats_secret": "..."}

    $ buildkite-agent secret batch-read --format env deploy_key github_api_token something_else_thats_secret
		DEPLOY_KEY="..."
		GITHUB_API_TOKEN="..."
		SOMETHING_ELSE_THATS_SECRET="..."
`,

	Flags: slices.Concat(globalFlags(), apiFlags(), []cli.Flag{
		cli.StringFlag{
			Name:   "job",
			Usage:  "Which job should should the secret be for",
			EnvVar: "BUILDKITE_JOB_ID",
		},
		cli.StringFlag{
			Name:   "format",
			Usage:  "The output format, either 'json' (default) or 'env'",
			Value:  "json",
			EnvVar: "BUILDKITE_AGENT_SECRET_BATCH_READ_FORMAT",
		},
		cli.BoolFlag{
			Name:   "skip-redaction",
			Usage:  "Skip redacting the retrieved secret from the logs. Then, the command will print the secret to the Job's logs if called directly.",
			EnvVar: "BUILDKITE_AGENT_SECRET_GET_SKIP_SECRET_REDACTION",
		},
	}),
	Action: func(c *cli.Context) error {
		ctx := context.Background()
		ctx, cfg, l, _, done := setupLoggerAndConfig[SecretBatchReadConfig](ctx, c)
		defer done()

		if len(cfg.Keys) == 0 {
			return errors.New("at least one secret key must be provided")
		}

		if cfg.Format != "json" && cfg.Format != "env" {
			return fmt.Errorf("invalid format %q: must be either 'json' or 'env'", cfg.Format)
		}

		agentClient := api.NewClient(l, loadAPIClientConfig(cfg, "AgentAccessToken"))
		secrets, errs := secrets.FetchSecrets(ctx, agentClient, cfg.Job, cfg.Keys, 20)
		if len(errs) > 0 {
			sb := &strings.Builder{}
			sb.WriteString("Failed to fetch some secrets:\n")
			for _, err := range errs {
				sb.WriteString(fmt.Sprintf(" - %s\n", err.Error()))
			}
			return errors.New(sb.String())
		}

		jobClient, err := jobapi.NewDefaultClient(ctx)
		if err != nil {
			return fmt.Errorf("failed to create Job API client: %w", err)
		}

		if !cfg.SkipRedaction {
			for _, secret := range secrets {
				if err := AddToRedactor(ctx, l, jobClient, secret.Value); err != nil {
					if cfg.Debug {
						return err
					}
					return errSecretRedact
				}
			}
		}

		secretsMap := make(map[string]string, len(secrets))
		for _, secret := range secrets {
			secretsMap[secret.Key] = secret.Value
		}

		switch cfg.Format {
		case "json":
			if err := json.NewEncoder(c.App.Writer).Encode(secretsMap); err != nil {
				return fmt.Errorf("failed to write JSON response: %w", err)
			}

		case "env":
			sb := strings.Builder{}
			for key, value := range secretsMap {
				sb.WriteString(fmt.Sprintf("%s=%q\n", strings.ToUpper(key), value))
			}

			_, _ = fmt.Fprint(c.App.Writer, sb.String())

		default:
			return fmt.Errorf("unsupported format %q", cfg.Format)
		}

		return nil
	},
}
