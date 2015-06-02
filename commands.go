package main

import (
	"github.com/buildkite/agent/command"
	"github.com/codegangsta/cli"
)

var Commands []cli.Command

func init() {
	Commands = []cli.Command{
		command.AgentStartCommand,
		{
			Name:  "artifact",
			Usage: "Upload/download artifacts from Buildkite jobs",
			Subcommands: []cli.Command{
				command.ArtifactUploadCommand,
				command.ArtifactDownloadCommand,
				command.ArtifactShasumCommand,
			},
		},
		{
			Name:  "meta-data",
			Usage: "Get/set data from Buildkite jobs",
			Subcommands: []cli.Command{
				command.MetaDataSetCommand,
				command.MetaDataGetCommand,
			},
		},
	}
}
