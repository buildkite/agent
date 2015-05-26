package command

import (
	"github.com/buildkite/agent/buildkite"
	"github.com/buildkite/agent/buildkite/logger"
	"github.com/codegangsta/cli"
)

func ArtifactUploadCommandAction(context *cli.Context) {
	c := buildkite.CLI{
		Context: context,
	}.Setup()

	c.Require("endpoint", "agent-access-token", "job")
	c.RequireArgs("upload paths")

	// See if an optional upload destination was supplied
	destination := ""
	if len(context.Args()) > 1 {
		destination = context.Args()[1]
	}

	var uploader = buildkite.ArtifactUploader{
		API: buildkite.API{
			Endpoint: context.String("endpoint"),
			Token:    context.String("agent-access-token"),
		},
		JobID:       context.String("job"),
		Paths:       context.Args()[0],
		Destination: destination,
	}

	err := uploader.Upload()
	if err != nil {
		logger.Fatal("Failed to upload artifacts: %s", err)
	}
}
