package clicommand

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/buildkite/agent/v3/api"
	"github.com/urfave/cli"
)

type SecretGetConfig struct {
	Key string `cli:"arg:0"`

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

var errJobIDNotSet = errors.New("BUILDKITE_JOB_ID not set")

var SecretGetCommand = cli.Command{
	Name:        "get",
	Usage:       "Get a secret by its key",
	Description: "Get a secret by key from Buildkite and print it to stdout.",
	Flags: []cli.Flag{
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

		client := api.NewClient(l, loadAPIClientConfig(cfg, "AgentAccessToken"))

		jobID := os.Getenv("BUILDKITE_JOB_ID")
		if jobID == "" {
			return NewExitError(1, errJobIDNotSet)
		}

		secret, _, err := client.GetSecret(ctx, &api.GetSecretRequest{Key: cfg.Key, JobID: jobID})
		if err != nil {
			return NewExitError(1, err)
		}

		_, err = fmt.Fprintln(c.App.Writer, secret.Value)

		return err
	},
}
