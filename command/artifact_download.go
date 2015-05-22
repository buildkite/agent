package command

import (
	"github.com/buildkite/agent/buildkite"
	"github.com/buildkite/agent/buildkite/logger"
	"github.com/codegangsta/cli"
)

func ArtifactDownloadCommandAction(context *cli.Context) {
	c := buildkite.CLI{
		Context: context,
	}.Setup()

	c.Require("endpoint", "agent-access-token", "job")
	c.RequireArgs("query", "download path")

	downloader := buildkite.ArtifactDownloader{
		API: buildkite.API{
			Endpoint: context.String("endpoint"),
			Token:    context.String("agent-access-token"),
		},
		JobID:       context.String("job"),
		Query:       context.Args()[0],
		Destination: context.Args()[1],
		Step:        context.String("step"),
	}

	err := downloader.Download()
	if err != nil {
		logger.Fatal("Failed to download artifacts: %s", err)
	}
}
