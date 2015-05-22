package command

import (
	"github.com/buildkite/agent/buildkite"
	"github.com/buildkite/agent/buildkite/logger"
	"github.com/codegangsta/cli"
)

func DataSetCommandAction(context *cli.Context) {
	c := buildkite.CLI{
		Context: context,
	}

	c.Setup()
	c.Require("endpoint", "agent-access-token", "job")

	var metaData = buildkite.MetaData{
		API: buildkite.API{
			Endpoint: context.String("endpoint"),
			Token:    context.String("agent-access-token"),
		},
		JobID: context.String("job"),
		Key:   context.Args()[0],
		Value: context.Args()[1],
	}

	err := metaData.Set()
	if err != nil {
		logger.Fatal("Failed to set meta-data: %s", err)
	}
}
