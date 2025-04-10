package clicommand

import (
	"context"
	"fmt"
	"slices"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/jobapi"
	"github.com/urfave/cli"
)

type SecretGetConfig struct {
	GlobalConfig
	APIConfig

	Key           string `cli:"arg:0"`
	Job           string `cli:"job" validate:"required"`
	SkipRedaction bool   `cli:"skip-redaction"`
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

Examples:

The following examples reference the same Buildkite secret ′key′:

    $ buildkite-agent secret get deploy_key
    $ buildkite-agent secret get DEPLOY_KEY`,
	Flags: slices.Concat(globalFlags(), apiFlags(), []cli.Flag{
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
	}),
	Action: func(c *cli.Context) error {
		ctx := context.Background()
		ctx, cfg, l, _, done := setupLoggerAndConfig[SecretGetConfig](ctx, c)
		defer done()

		agentClient := api.NewClient(l, loadAPIClientConfig(cfg, "AgentAccessToken"))
		secret, _, err := agentClient.GetSecret(ctx, &api.GetSecretRequest{Key: cfg.Key, JobID: cfg.Job})
		if err != nil {
			return err
		}

		jobClient, err := jobapi.NewDefaultClient(ctx)
		if err != nil {
			return fmt.Errorf("failed to create Job API client: %w", err)
		}

		if !cfg.SkipRedaction {
			if err := AddToRedactor(ctx, l, jobClient, secret.Value); err != nil {
				if cfg.Debug {
					return err
				}
				return errSecretRedact
			}
		}

		_, err = fmt.Fprintln(c.App.Writer, secret.Value)

		return err
	},
}
