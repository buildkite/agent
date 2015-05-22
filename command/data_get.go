package command

import (
	"fmt"
	"github.com/buildkite/agent/buildkite"
	"github.com/buildkite/agent/buildkite/logger"
	"github.com/codegangsta/cli"
)

func DataGetCommandAction(context *cli.Context) {
	c := buildkite.CLI{
		Context: context,
	}.Setup()

	c.Require("endpoint", "agent-access-token", "job")

	var metaData = buildkite.MetaData{
		API: buildkite.API{
			Endpoint: context.String("endpoint"),
			Token:    context.String("agent-access-token"),
		},
		JobID: context.String("job"),
		Key:   context.Args()[0],
	}

	err := metaData.Get()
	if err != nil {
		logger.Fatal("Failed to get meta-data: %s", err)
	}

	// Output the value to STDOUT
	fmt.Print(metaData.Value)
}
