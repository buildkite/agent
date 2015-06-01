package command

import (
	"fmt"
	"github.com/buildkite/agent/buildkite"
	"github.com/buildkite/agent/buildkite/logger"
	"github.com/codegangsta/cli"
)

func ArtifactShasumCommandAction(context *cli.Context) {
	c := buildkite.CLI{
		Context: context,
	}.Setup()

	c.Require("endpoint", "agent-access-token", "build")
	c.RequireArgs("query")

	if context.String("job") != "" {
		logger.Fatal("--job is deprecated. Please use --step")
	}

	// Find the artifact we want to show the SHASUM for
	searcher := buildkite.ArtifactSearcher{
		BuildID: context.String("build"),
		API: buildkite.API{
			Endpoint: context.String("endpoint"),
			Token:    context.String("agent-access-token"),
		},
	}

	err := searcher.Search(context.Args()[0], context.String("step"))
	if err != nil {
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
}
