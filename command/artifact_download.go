package command

import (
	"github.com/buildkite/agent/buildkite"
	"github.com/buildkite/agent/cliconfig"
	"github.com/buildkite/agent/logger"
	"github.com/codegangsta/cli"
)

var DownloadHelpDescription = `Usage:

   buildkite-agent artifact download [arguments...]

Description:

   Downloads artifacts from Buildkite to the local machine.

   Note: You need to ensure that your search query is surrounded by quotes if
   using a wild card as the built-in shell path globbing will provide files,
   which will break the download.

Example:

   $ buildkite-agent artifact download "pkg/*.tar.gz" . --build xxx

   This will search across all the artifacts for the build with files that match that part.
   The first argument is the search query, and the second argument is the download destination.

   If you're trying to download a specific file, and there are multiple artifacts from different
   jobs, you can target the particular job you want to download the artifact from:

   $ buildkite-agent artifact download "pkg/*.tar.gz" . --step "tests" --build xxx

   You can also use the step's jobs id (provided by the environment variable $BUILDKITE_JOB_ID)`

type ArtifactDownloadConfig struct {
	Query            string `cli:"arg:0" label:"artifact search query" validate:"required"`
	Destination      string `cli:"arg:1" label:"artifact download path" validate:"required"`
	Step             string `cli:"step"`
	Job              string `cli:"job" deprecated:"--job is deprecated. Please use --step"`
	Build            string `cli:"build" validate:"required"`
	AgentAccessToken string `cli:"agent-access-token" validate:"required"`
	Endpoint         string `cli:"endpoint" validate:"required"`
	NoColor          bool   `cli:"no-color"`
	Debug            bool   `cli:"debug"`
}

var ArtifactDownloadCommand = cli.Command{
	Name:        "download",
	Usage:       "Downloads artifacts from Buildkite to the local machine",
	Description: DownloadHelpDescription,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:  "job",
			Value: "",
			Usage: "DEPRECATED",
		},
		cli.StringFlag{
			Name:  "step",
			Value: "",
			Usage: "Scope the search to a paticular step by using either it's name of job ID",
		},
		cli.StringFlag{
			Name:   "build",
			Value:  "",
			EnvVar: "BUILDKITE_BUILD_ID",
			Usage:  "The build that the artifacts were uploaded to",
		},
		AgentAccessTokenFlag,
		EndpointFlag,
		DebugFlag,
		NoColorFlag,
	},
	Action: func(c *cli.Context) {
		// The configuration will be loaded into this struct
		cfg := ArtifactDownloadConfig{}

		// Load the configuration
		if err := cliconfig.Load(c, &cfg); err != nil {
			logger.Fatal("%s", err)
		}

		// Setup the any global configuration options
		HandleGlobalFlags(cfg)

		// Setup the downloader
		downloader := buildkite.ArtifactDownloader{
			APIClient: buildkite.APIClient{
				Endpoint: cfg.Endpoint,
				Token:    cfg.AgentAccessToken,
			}.Create(),
			Query:       cfg.Query,
			Destination: cfg.Destination,
			BuildID:     cfg.Build,
			Step:        cfg.Step,
		}

		// Download the artifacts
		if err := downloader.Download(); err != nil {
			logger.Fatal("Failed to download artifacts: %s", err)
		}
	},
}
