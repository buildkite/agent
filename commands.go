package main

import (
	"github.com/buildkite/agent/command"
	"github.com/codegangsta/cli"
)

var Commands []cli.Command

var SetHelpDescription = `Usage:

   buildkite-agent meta-data set <key> <value> [arguments...]

Description:

   Set arbitrary data on a build using a basic key/value store.

Example:

   $ buildkite-agent meta-data set "foo" "bar"`

var GetHelpDescription = `Usage:

   buildkite-agent meta-data get <key> [arguments...]

Description:

   Get data from a builds key/value store.

Example:

   $ buildkite-agent meta-data get "foo"`

var DefaultEndpoint = "https://agent.buildkite.com/v2"

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
				{
					Name:        "set",
					Usage:       "Set data on a build",
					Description: SetHelpDescription,
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:   "job",
							Value:  "",
							Usage:  "Which job should the meta-data be set on",
							EnvVar: "BUILDKITE_JOB_ID",
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
					Action: command.DataSetCommandAction,
				},
				{
					Name:        "get",
					Usage:       "Get data from a build",
					Description: GetHelpDescription,
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:   "job",
							Value:  "",
							Usage:  "Which job should the meta-data be retrieved from",
							EnvVar: "BUILDKITE_JOB_ID",
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
					Action: command.DataGetCommandAction,
				},
			},
		},
	}
}
