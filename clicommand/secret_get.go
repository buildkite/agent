package clicommand

import (
	"context"
	"fmt"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/jobapi"
	"github.com/urfave/cli"
)

type SecretGetConfig struct {
	Key string `cli:"arg:0"`
	Job string `cli:"job" validate:"required"`

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
	Name:        "get",
	Usage:       "Get a secret by its key",
	Description: "Get a secret by key from Buildkite and print it to stdout.",
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:   "job",
			Usage:  "Which job should should the secret be for",
			EnvVar: "BUILDKITE_JOB_ID",
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

		jobClient, err := jobapi.NewDefaultClient(ctx)
		if err != nil {
			return fmt.Errorf("failed to create Job API client: %w", err)
		}

		if err := AddToRedactor(ctx, l, jobClient, secret.Value); err != nil {
			if cfg.Debug {
				return err
			}
			return errSecretRedact
		}

		_, err = fmt.Fprintln(c.App.Writer, secret.Value)

		return err
	},
}
