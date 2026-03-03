package clicommand

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/internal/secrets"
	"github.com/buildkite/agent/v3/jobapi"
	"github.com/urfave/cli"
)

type SecretGetConfig struct {
	GlobalConfig
	APIConfig

	Keys          []string `cli:"arg:*"`
	Format        string   `cli:"format"`
	Job           string   `cli:"job" validate:"required"`
	SkipRedaction bool     `cli:"skip-redaction"`
}

var SecretGetCommand = cli.Command{
	Name:  "get",
	Usage: "Get a list of secrets by their keys and print them to stdout",
	Description: `Usage:

    buildkite-agent secret get [options...] [key1] [key2] ...

Description:

Gets a list of secrets from Buildkite and prints them to stdout. Key names are case
insensitive in this command, and secret values are automatically redacted in the build logs
unless the ′skip-redaction′ flag is used.

If any request for a secret fails, the command will return a non-zero exit code and print
details of all failed secrets.

By default, when fetching a single key, the secret value will be printed without any
formatting. When fetching multiple keys, the output will be in JSON format. Output
format can be controlled explicitly with the ′format′ flag.

Examples:

    # Secret keys are case insensitive
    $ buildkite-agent secret get deploy_key
    "..."
    $ buildkite-agent secret get DEPLOY_KEY
    "..."

    # The return value can also be formatted using env (which can be piped
    # into e.g. ′source′, ′declare -x′), or json
    $ buildkite-agent secret get --format env deploy_key github_api_token
    DEPLOY_KEY="..."
    GITHUB_API_TOKEN="..."

    $ buildkite-agent secret get --format json deploy_key github_api_token
    {"deploy_key": "...", "github_api_token": "..."}
`,
	Flags: slices.Concat(globalFlags(), apiFlags(), []cli.Flag{
		cli.StringFlag{
			Name:   "job",
			Usage:  "Which job should should the secret be for",
			EnvVar: "BUILDKITE_JOB_ID",
		},
		cli.StringFlag{
			Name:   "format",
			Usage:  "The output format, either 'default', 'json', or 'env'. When 'default', a single secret will print just the value, while multiple secrets will print JSON. When 'json' or 'env', secrets will be printed as key-value pairs in the requested format",
			Value:  "default",
			EnvVar: "BUILDKITE_AGENT_SECRET_GET_FORMAT",
		},
		cli.BoolFlag{
			Name:   "skip-redaction",
			Usage:  "Skip redacting the retrieved secret from the logs. Then, the command will print the secret to the Job's logs if called directly (default: false)",
			EnvVar: "BUILDKITE_AGENT_SECRET_GET_SKIP_SECRET_REDACTION",
		},
	}),
	Action: func(c *cli.Context) error {
		ctx := context.Background()
		ctx, cfg, l, _, done := setupLoggerAndConfig[SecretGetConfig](ctx, c)
		defer done()

		if len(cfg.Keys) == 0 {
			return errors.New("at least one secret key must be provided")
		}

		if !slices.Contains([]string{"default", "json", "env"}, cfg.Format) {
			return fmt.Errorf("invalid format %q: must be one of 'default', 'json', or 'env'", cfg.Format)
		}

		agentClient := api.NewClient(l, loadAPIClientConfig(cfg, "AgentAccessToken"))
		secrets, errs := secrets.FetchSecrets(ctx, l, agentClient, cfg.Job, cfg.Keys, 10)
		if len(errs) > 0 {
			sb := &strings.Builder{}
			sb.WriteString("Failed to fetch some secrets:\n")
			for _, err := range errs {
				_, _ = fmt.Fprintf(sb, " - %v\n", err)
			}
			return errors.New(sb.String())
		}

		if !cfg.SkipRedaction {
			jobClient, err := jobapi.NewDefaultClient(ctx)
			if err != nil {
				return fmt.Errorf("failed to create Job API client: %w", err)
			}

			for _, secret := range secrets {
				if err := AddToRedactor(ctx, l, jobClient, secret.Value); err != nil {
					if cfg.Debug {
						return err
					}
					return errSecretRedact
				}
			}
		}

		// Otherwise, print in the requested format
		secretsMap := make(map[string]string, len(secrets))
		for _, secret := range secrets {
			secretsMap[secret.Key] = secret.Value
		}

		switch {
		case len(cfg.Keys) == 1 && cfg.Format == "default":
			_, _ = fmt.Fprintln(c.App.Writer, secrets[0].Value)
			return nil

		case cfg.Format == "json" || (cfg.Format == "default" && len(cfg.Keys) > 1):
			if err := json.NewEncoder(c.App.Writer).Encode(secretsMap); err != nil {
				return fmt.Errorf("failed to write JSON response: %w", err)
			}

		case cfg.Format == "env":
			for _, key := range slices.Sorted(maps.Keys(secretsMap)) {
				fmt.Fprintf(c.App.Writer, "%s=%q\n", strings.ToUpper(key), secretsMap[key]) //nolint:errcheck // CLI output; errors are non-actionable
			}

		default:
			return fmt.Errorf("unsupported format %q", cfg.Format)
		}

		return nil
	},
}
