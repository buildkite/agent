package command

import (
	"fmt"
	"github.com/buildkite/agent/buildkite"
	"github.com/buildkite/agent/cliconfig"
	"github.com/buildkite/agent/logger"
	"github.com/codegangsta/cli"
)

var ShasumHelpDescription = `Usage:

   buildkite-agent artifact shasum [arguments...]

Description:

   Prints to STDOUT the SHA-1 for the artifact provided. If your search query
   for artifacts matches multiple agents, and error will be raised.

   Note: You need to ensure that your search query is surrounded by quotes if
   using a wild card as the built-in shell path globbing will provide files,
   which will break the download.

Example:

   $ buildkite-agent artifact shasum "pkg/release.tar.gz" --build xxx

   This will search for all the files in the build with the path "pkg/release.tar.gz" and will
   print to STDOUT it's SHA-1 checksum.

   If you would like to target artifacts from a specific build step, you can do
   so by using the --step argument.

   $ buildkite-agent artifact shasum "pkg/release.tar.gz" --step "release" --build xxx

   You can also use the step's job id (provided by the environment variable $BUILDKITE_JOB_ID)`

type ArtifactShasumConfig struct {
	Query            string `cli:"arg:0" label:"artifact search query" validate:"required"`
	Step             string `cli:"step"`
	Job              string `cli:"job" deprecated:"--job is deprecated. Please use --step"`
	Build            string `cli:"build" validate:"required"`
	AgentAccessToken string `cli:"agent-access-token" validate:"required"`
	Endpoint         string `cli:"endpoint" validate:"required"`
	NoColor          bool   `cli:"no-color"`
	Debug            bool   `cli:"debug"`
}

var ArtifactShasumCommand = cli.Command{
	Name:        "shasum",
	Usage:       "Prints the SHA-1 checksum for the artifact provided to STDOUT",
	Description: ShasumHelpDescription,
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
		cli.StringFlag{
			Name:   "agent-access-token",
			Value:  "",
			Usage:  "The access token used to identify the agent",
			EnvVar: "BUILDKITE_AGENT_ACCESS_TOKEN",
		},
		cli.StringFlag{
			Name:   "endpoint",
			Value:  DefaultEndpoint,
			Usage:  "The Agent API endpoint",
			EnvVar: "BUILDKITE_AGENT_ENDPOINT",
		},
		cli.BoolFlag{
			Name:   "debug",
			Usage:  "Enable debug mode",
			EnvVar: "BUILDKITE_AGENT_DEBUG",
		},
		cli.BoolFlag{
			Name:   "no-color",
			Usage:  "Don't show colors in logging",
			EnvVar: "BUILDKITE_AGENT_NO_COLOR",
		},
	},
	Action: func(c *cli.Context) {
		// The configuration will be loaded into this struct
		cfg := ArtifactShasumConfig{}

		// Load the configuration
		if err := cliconfig.Load(c, &cfg); err != nil {
			logger.Fatal("%s", err)
		}

		// Setup the any global configuration options
		SetupGlobalConfiguration(cfg)

		// Find the artifact we want to show the SHASUM for
		searcher := buildkite.ArtifactSearcher{
			BuildID: cfg.Build,
			API: buildkite.API{
				Endpoint: cfg.Endpoint,
				Token:    cfg.AgentAccessToken,
			},
		}

		if err := searcher.Search(cfg.Query, cfg.Step); err != nil {
			logger.Fatal("Failed to find artifacts: %s", err)
		}

		artifactsFoundLength := len(searcher.Artifacts)

		if artifactsFoundLength == 0 {
			logger.Fatal("No artifacts found for downloading")
		} else if artifactsFoundLength > 1 {
			logger.Fatal("Multiple artifacts were found. Try being more specific with the search or scope by step")
		} else {
			logger.Debug("Artifact \"%s\" found", searcher.Artifacts[0].Path)

			fmt.Printf("%s\n", searcher.Artifacts[0].Sha1Sum)
		}
	},
}
