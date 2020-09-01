package clicommand

import (
	"github.com/buildkite/agent/v3/agent"
	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/cliconfig"
	"github.com/urfave/cli/v2"
)

var SearchHelpDescription = `Usage:

   buildkite-agent artifact search [options] <query> <destination>

Description:  ## TODO CHECK

	 Searches for build artifacts specified by <query> on Buildkite

   Note: You need to ensure that your search query is surrounded by quotes if
   using a wild card as the built-in shell path globbing will provide files,
   which will break the search.

Example: ## TODO CHECK

   $ buildkite-agent artifact search "pkg/*.tar.gz" --build xxx

   This will search across all the artifacts for the build for files that match that query.
   The first argument is the search query.

   If you're trying to find a specific file, and there are multiple artifacts from different
   jobs, you can target the particular job you want to search the artifact using --step:

   $ buildkite-agent artifact search "pkg/*.tar.gz" --step "tests" --build xxx

   You can also use the step's job id (provided by the environment variable $BUILDKITE_JOB_ID)`

type ArtifactSearchConfig struct {
	Query              string `cli:"arg:0" label:"artifact search query" validate:"required"`
	Step               string `cli:"step"`
	Build              string `cli:"build" validate:"required"`
	IncludeRetriedJobs bool   `cli:"include-retried-jobs"`

	// Global flags
	Debug       bool     `cli:"debug"`
	NoColor     bool     `cli:"no-color"`
	Experiments []string `cli:"experiment" normalize:"list"`
	Profile     string   `cli:"profile"`

	// API config
	DebugHTTP        bool   `cli:"debug-http"`
	AgentAccessToken string `cli:"agent-access-token" validate:"required"`
	Endpoint         string `cli:"endpoint" validate:"required"`
	NoHTTP2          bool   `cli:"no-http2"`
}

var ArtifactSearchCommand = cli.Command{
	Name:        "search",
	Usage:       "Searches artifacts in Buildkite",
	Description: SearchHelpDescription,
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "step",
			Value: "",
			Usage: "Scope the search to a particular step by using either its name or job ID",
		},
		&cli.StringFlag{
			Name:    "build",
			Value:   "",
			EnvVars: []string{"BUILDKITE_BUILD_ID"},
			Usage:   "The build that the artifacts were uploaded to",
		},
		&cli.BoolFlag{
			Name:    "include-retried-jobs",
			EnvVars: []string{"BUILDKITE_AGENT_INCLUDE_RETRIED_JOBS"},
			Usage:   "Include artifacts from retried jobs in the search",
		},

		// API Flags
		AgentAccessTokenFlag,
		EndpointFlag,
		NoHTTP2Flag,
		DebugHTTPFlag,

		// Global flags
		NoColorFlag,
		DebugFlag,
		ExperimentsFlag,
		ProfileFlag,
	},
	Action: func(c *cli.Context) error {
		// The configuration will be loaded into this struct
		cfg := ArtifactSearchConfig{}

		l := CreateLogger(&cfg)

		// Load the configuration
		if err := cliconfig.Load(c, l, &cfg); err != nil {
			l.Fatal("%s", err)
		}

		// Setup any global configuration options
		done := HandleGlobalFlags(l, cfg)
		defer done()

		// Create the API client
		client := api.NewClient(l, loadAPIClientConfig(cfg, `AgentAccessToken`))

		// Setup the downloader
		downloader := agent.NewArtifactDownloader(l, client, agent.ArtifactDownloaderConfig{
			Query:              cfg.Query,
			Destination:        cfg.Destination,
			BuildID:            cfg.Build,
			Step:               cfg.Step,
			IncludeRetriedJobs: cfg.IncludeRetriedJobs,
			DebugHTTP:          cfg.DebugHTTP,
		})

		// Download the artifacts
		if err := downloader.Download(); err != nil {
			l.Fatal("Failed to download artifacts: %s", err)
		}

		return nil
	},
}
