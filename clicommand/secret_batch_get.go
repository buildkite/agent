package clicommand

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/jobapi"
	"github.com/urfave/cli"
)

type SecretBatchGetConfig struct {
	GlobalConfig
	APIConfig

	Keys          []string `cli:"arg"`
	Job           string   `cli:"job" validate:"required"`
	SkipRedaction bool     `cli:"skip-redaction"`
	Format        string   `cli:"format"`
	KeysFromFile  string   `cli:"keys-from-file"`
}

var SecretBatchGetCommand = cli.Command{
	Name:  "batch-get",
	Usage: "Get multiple secrets by their keys and print them to stdout",
	Description: `Usage:

    buildkite-agent secret batch-get [key1] [key2] [key3] ... [options...]
    buildkite-agent secret batch-get --keys-from-file /path/to/keyfile [options...]

Description:

Gets multiple secrets from Buildkite secrets and prints them to stdout. The keys
specified in this command are the key names defined for the secrets in the
cluster. The key names are case insensitive in this command, and the
key values are automatically redacted in the build logs.

You can either provide keys as command line arguments or use a file containing
one key per line with the --keys-from-file option.

Examples:

The following examples fetch multiple secrets:

    $ buildkite-agent secret batch-get deploy_key api_token db_password
    $ buildkite-agent secret batch-get --keys-from-file secrets.txt
    $ buildkite-agent secret batch-get deploy_key api_token --format env`,
	Flags: slices.Concat(globalFlags(), apiFlags(), []cli.Flag{
		cli.StringFlag{
			Name:   "job",
			Usage:  "Which job should the secrets be for",
			EnvVar: "BUILDKITE_JOB_ID",
		},
		cli.BoolFlag{
			Name:   "skip-redaction",
			Usage:  "Skip redacting the retrieved secrets from the logs. Then, the command will print the secrets to the Job's logs if called directly.",
			EnvVar: "BUILDKITE_AGENT_SECRET_BATCH_GET_SKIP_SECRET_REDACTION",
		},
		cli.StringFlag{
			Name:   "format",
			Value:  "env",
			Usage:  "Output format: 'env' for environment variables, 'json' for JSON output",
			EnvVar: "BUILDKITE_AGENT_SECRET_BATCH_GET_FORMAT",
		},
		cli.StringFlag{
			Name:   "keys-from-file",
			Usage:  "Read secret keys from file (one key per line)",
			EnvVar: "BUILDKITE_AGENT_SECRET_BATCH_GET_KEYS_FROM_FILE",
		},
	}),
	Action: func(c *cli.Context) error {
		ctx := context.Background()
		ctx, cfg, l, _, done := setupLoggerAndConfig[SecretBatchGetConfig](ctx, c)
		defer done()

		var keys []string
		if cfg.KeysFromFile != "" {
			// Read keys from file
			contentBytes, err := os.ReadFile(cfg.KeysFromFile)
			if err != nil {
				return fmt.Errorf("failed to read keys from file %s: %w", cfg.KeysFromFile, err)
			}
			content := string(contentBytes)
			lines := strings.Split(strings.TrimSpace(content), "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line != "" && !strings.HasPrefix(line, "#") {
					keys = append(keys, line)
				}
			}
		} else {
			keys = cfg.Keys
		}

		if len(keys) == 0 {
			return fmt.Errorf("no secret keys provided. Use either command line arguments or --keys-from-file")
		}

		agentClient := api.NewClient(l, loadAPIClientConfig(cfg, "AgentAccessToken"))
		secretsResp, _, err := agentClient.GetSecrets(ctx, &api.GetSecretsRequest{
			Keys:  keys,
			JobID: cfg.Job,
		})
		if err != nil {
			return err
		}

		jobClient, err := jobapi.NewDefaultClient(ctx)
		if err != nil {
			return fmt.Errorf("failed to create Job API client: %w", err)
		}

		// Add all secret values to redactor unless skipped
		if !cfg.SkipRedaction {
			for _, secret := range secretsResp.Secrets {
				if err := AddToRedactor(ctx, l, jobClient, secret.Value); err != nil {
					if cfg.Debug {
						return err
					}
					return errSecretRedact
				}
			}
		}

		// Output secrets based on format
		switch cfg.Format {
		case "env":
			for _, secret := range secretsResp.Secrets {
				_, err = fmt.Fprintf(c.App.Writer, "%s=%s\n", secret.Key, secret.Value)
				if err != nil {
					return err
				}
			}
		case "json":
			// Simple JSON output
			fmt.Fprintf(c.App.Writer, "{\n")
			for i, secret := range secretsResp.Secrets {
				if i > 0 {
					fmt.Fprintf(c.App.Writer, ",\n")
				}
				fmt.Fprintf(c.App.Writer, "  %q: %q", secret.Key, secret.Value)
			}
			fmt.Fprintf(c.App.Writer, "\n}\n")
		default:
			return fmt.Errorf("unknown format: %s. Supported formats: env, json", cfg.Format)
		}

		return nil
	},
}
