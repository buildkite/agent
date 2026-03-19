package clicommand

import (
	"context"
	"fmt"
	"io"
	"os"
	"slices"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/internal/artifact"
	"github.com/buildkite/agent/v3/logger"
	"github.com/urfave/cli"
)

const shasumHelpDescription = `Usage:

    buildkite-agent artifact shasum [options...]

Description:

Prints the SHA-1 or SHA-256 hash for the single artifact specified by a
search query.

The hash is fetched from Buildkite's API, having been generated client-side
by the agent during artifact upload.

A search query that does not match exactly one artifact results in an error.

Note: You need to ensure that your search query is surrounded by quotes if
using a wild card as the built-in shell path globbing will provide files,
which will break the download.

Example:

    $ buildkite-agent artifact shasum "pkg/release.tar.gz" --build xxx

This will search for all files in the build with path "pkg/release.tar.gz",
and if exactly one match is found, the SHA-1 hash generated during upload
is printed.

If you would like to target artifacts from a specific build step, you can do
so by using the --step argument.

    $ buildkite-agent artifact shasum "pkg/release.tar.gz" --step "release" --build xxx

You can also use the step's job ID (provided by the environment variable $BUILDKITE_JOB_ID)

The ′--sha256′ argument requests SHA-256 instead of SHA-1; this is only
available for artifacts uploaded since SHA-256 support was added to the
agent.`

type ArtifactShasumConfig struct {
	GlobalConfig
	APIConfig

	Query              string `cli:"arg:0" label:"artifact search query" validate:"required"`
	Sha256             bool   `cli:"sha256"`
	Step               string `cli:"step"`
	Build              string `cli:"build" validate:"required"`
	IncludeRetriedJobs bool   `cli:"include-retried-jobs"`
}

var ArtifactShasumCommand = cli.Command{
	Name:        "shasum",
	Usage:       "Prints the SHA-1 hash for a single artifact specified by a search query",
	Description: shasumHelpDescription,
	Flags: slices.Concat(globalFlags(), apiFlags(), []cli.Flag{
		cli.BoolFlag{
			Name:  "sha256",
			Usage: "Request SHA-256 instead of SHA-1, errors if SHA-256 not available (default: false)",
		},
		cli.StringFlag{
			Name:  "step",
			Value: "",
			Usage: "Scope the search to a particular step by its name or job ID",
		},
		cli.StringFlag{
			Name:   "build",
			Value:  "",
			EnvVar: "BUILDKITE_BUILD_ID",
			Usage:  "The build that the artifact was uploaded to",
		},
		cli.BoolFlag{
			Name:   "include-retried-jobs",
			EnvVar: "BUILDKITE_AGENT_INCLUDE_RETRIED_JOBS",
			Usage:  "Include artifacts from retried jobs in the search (default: false)",
		},
	}),
	Action: func(c *cli.Context) error {
		ctx := context.Background()
		ctx, cfg, l, _, done := setupLoggerAndConfig[ArtifactShasumConfig](ctx, c)
		defer done()
		return searchAndPrintShaSum(ctx, cfg, l, os.Stdout)
	},
}

func searchAndPrintShaSum(
	ctx context.Context,
	cfg ArtifactShasumConfig,
	l logger.Logger,
	stdout io.Writer,
) error {
	// Create the API client
	client := api.NewClient(l, loadAPIClientConfig(cfg, "AgentAccessToken"))

	// Find the artifact we want to show the SHASUM for
	searcher := artifact.NewSearcher(l, client, cfg.Build)
	artifacts, err := searcher.Search(ctx, cfg.Query, cfg.Step, cfg.IncludeRetriedJobs, false)
	if err != nil {
		return fmt.Errorf("Error searching for artifacts: %s", err)
	}

	artifactsFoundLength := len(artifacts)

	if artifactsFoundLength == 0 {
		return fmt.Errorf("No artifacts matched the search query")
	} else if artifactsFoundLength > 1 {
		return fmt.Errorf("Multiple artifacts were found. Try being more specific with the search or scope by step")
	} else {
		a := artifacts[0]
		l.Debug("Artifact \"%s\" found", a.Path)

		var sha string
		if cfg.Sha256 {
			if a.Sha256Sum == "" {
				return fmt.Errorf("SHA-256 requested but was not generated at upload time")
			}
			sha = a.Sha256Sum
		} else {
			sha = a.Sha1Sum
		}
		fmt.Fprintln(stdout, sha)
	}

	return nil
}
