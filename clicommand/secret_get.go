package clicommand

import (
	"fmt"
	"os"

	"github.com/buildkite/agent/cliconfig"
	"github.com/buildkite/agent/logger"
	"github.com/segmentio/chamber/store"
	"github.com/urfave/cli"
)

var SecretGetHelpDescription = `Usage:

   buildkite-agent secret get <key> [arguments...]

Description:

   Get a secret from the secret store configured in the agent

Example:

   $ buildkite-agent secret get "foo"`

type SecretGetConfig struct {
	Key          string `cli:"arg:0" label:"secret key" validate:"required"`
	OrgSlug      string `cli:"org-slug" validate:"required"`
	PipelineSlug string `cli:"pipeline-slug"`
	Scope        string `cli:"scope"`
	Version      int    `cli:"version"`
	NoColor      bool   `cli:"no-color"`
	Debug        bool   `cli:"debug"`
	DebugHTTP    bool   `cli:"debug-http"`
}

var SecretGetCommand = cli.Command{
	Name:        "get",
	Usage:       "Get a secret from the secret store",
	Description: MetaDataGetHelpDescription,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:   "org-slug",
			Value:  "",
			Usage:  "Which organization should be used in the service key",
			EnvVar: "BUILDKITE_ORG_SLUG",
		},
		cli.StringFlag{
			Name:   "pipeline-slug",
			Value:  "",
			Usage:  "Which pipeline should be used in the service key",
			EnvVar: "BUILDKITE_PIPELINE_SLUG",
		},
		cli.StringFlag{
			Name:   "scope",
			Value:  "pipeline",
			Usage:  "The scope for the secret, either pipeline (default) or organization",
			EnvVar: "BUILDKITE_SECRET_SCOPE",
		},
		cli.IntFlag{
			Name:  "version",
			Value: -1,
			Usage: "The version number of the secret. Defaults to latest.",
		},
		AgentAccessTokenFlag,
		EndpointFlag,
		NoColorFlag,
		DebugFlag,
		DebugHTTPFlag,
	},
	Action: func(c *cli.Context) {
		// The configuration will be loaded into this struct
		cfg := SecretGetConfig{}

		// Load the configuration
		if err := cliconfig.Load(c, &cfg); err != nil {
			logger.Fatal("%s", err)
		}

		var service string

		// service is going to be either buildkite/{org}/{pipeline} or buildkite/{org}
		switch cfg.Scope {
		case "organization", "organisation":
			service = fmt.Sprintf("buildkite_%s", cfg.OrgSlug)
		case "pipeline":
			service = fmt.Sprintf("buildkite_%s_%s", cfg.OrgSlug, cfg.PipelineSlug)
		default:
			logger.Fatal("Unknown scope %q", cfg.Scope)
		}

		secretStore := store.NewSSMStore()
		secretId := store.SecretId{
			Service: service,
			Key:     cfg.Key,
		}

		secret, err := secretStore.Read(secretId, cfg.Version)
		if err != nil {
			logger.Fatal("Failed to read: %v", err)
		}

		fmt.Fprintf(os.Stdout, "%s", *secret.Value)
	},
}
