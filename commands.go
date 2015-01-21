package main

import (
	"github.com/buildkite/agent/buildkite"
	"github.com/buildkite/agent/command"
	"github.com/codegangsta/cli"
)

var Commands []cli.Command

var AgentDescription = `Usage:

   buildkite-agent start [arguments...]

Description:

   When a job is ready to run it will call the "bootstrap-script"
   and pass it all the environment variables required for the job to run.
   This script is responsible for checking out the code, and running the
   actual build script defined in the project.

   The agent will run any jobs within a PTY (pseudo terminal) if available.

Example:

   $ buildkite-agent start --token xxx`

var DownloadHelpDescription = `Usage:

   buildkite-agent build-artifact download [arguments...]

Description:

   Downloads artifacts from Buildkite to the local machine.

   You need to ensure that your search query is surrounded by quotes if
   using a wild card as the built-in shell path globbing will provide files,
   which will break the download.

Example:

   $ buildkite-agent build-artifact download "pkg/*.tar.gz" . --build xxx

   This will search across all the artifacts for the build with files that match that part.
   The first argument is the search query, and the second argument is the download destination.

   If you're trying to download a specific file, and there are multiple artifacts from different
   jobs, you can target the paticular job you want to download the artifact from:

   $ buildkite-agent build-artifact download "pkg/*.tar.gz" . --job "tests" --build xxx

   You can also use the job's id (provided by the environment variable $BUILDKITE_JOB_ID)`

var UploadHelpDescription = `Usage:

   buildkite-agent build-artifact upload <pattern> <destination> [arguments...]

Description:

   Uploads files to a job as artifacts.

   You need to ensure that the paths are surrounded by quotes otherwise the
   built-in shell path globbing will provide the files, which is currently not
   supported.

Example:

   $ buildkite-agent build-artifact upload "log/**/*.log"

   You can also upload directy to Amazon S3 if you'd like to host your own artifacts:

   $ export AWS_SECRET_ACCESS_KEY=yyy
   $ export AWS_ACCESS_KEY_ID=xxx
   $ buildkite-agent build-artifact upload "log/**/*.log" s3://name-of-your-s3-bucket/$BUILDKITE_JOB_ID`

var SetHelpDescription = `Usage:

   buildkite-agent build-data set <key> <value> [arguments...]

Description:

   Set arbitrary data on a build using a basic key/value store.

Example:

   $ buildkite-agent build-data set "foo" "bar"`

var GetHelpDescription = `Usage:

   buildkite-agent build-data get <key> [arguments...]

Description:

   Get data from a builds key/value store.

Example:

   $ buildkite-agent build-data get "foo"`

func init() {
	// This is default location of the bootstrap for unix based operating
	// systems.
	bootstrapScriptLocation := "$HOME/.buildkite/bootstrap.sh"

	// Windows has a slightly modified default bootstrap location
	if buildkite.MachineIsWindows() {
		bootstrapScriptLocation = "$USERPROFILE\\AppData\\Local\\BuildkiteAgent\\bootstrap.bat"
	}

	Commands = []cli.Command{
		{
			Name:        "start",
			Usage:       "Starts a Buildkite agent",
			Description: AgentDescription,
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:   "token",
					Value:  "",
					Usage:  "Your account agent token",
					EnvVar: "BUILDKITE_AGENT_TOKEN",
				},
				cli.StringFlag{
					Name:   "access-token",
					Value:  "",
					Usage:  "DEPRECATED: The agents access token",
					EnvVar: "BUILDKITE_AGENT_ACCESS_TOKEN",
				},
				cli.StringFlag{
					Name:   "name",
					Value:  "",
					Usage:  "The name of the agent",
					EnvVar: "BUILDKITE_AGENT_NAME",
				},
				cli.StringFlag{
					Name:   "priority",
					Value:  "",
					Usage:  "The priority of the agent",
					EnvVar: "BUILDKITE_AGENT_PRIORITY",
				},
				cli.StringSliceFlag{
					Name:   "meta-data",
					Value:  &cli.StringSlice{},
					Usage:  "Meta data for the agent",
					EnvVar: "BUILDKITE_AGENT_META_DATA",
				},
				cli.StringFlag{
					Name:   "bootstrap-script",
					Value:  bootstrapScriptLocation,
					Usage:  "Path to the bootstrap script",
					EnvVar: "BUILDKITE_BOOTSTRAP_SCRIPT_PATH",
				},
				cli.StringFlag{
					Name:   "endpoint",
					Value:  "https://agent.buildkite.com/v2",
					Usage:  "The agent API endpoint",
					EnvVar: "BUILDKITE_AGENT_ENDPOINT",
				},
				cli.BoolFlag{
					Name:  "meta-data-ec2-tags",
					Usage: "Populate the meta data from the current instances EC2 Tags",
				},
				cli.BoolFlag{
					Name:  "no-pty",
					Usage: "Do not run jobs within a pseudo terminal",
				},
				cli.BoolFlag{
					Name:   "debug",
					Usage:  "Enable debug mode.",
					EnvVar: "BUILDKITE_AGENT_DEBUG",
				},
			},
			Action: command.AgentStartCommandAction,
		},
		{
			Name:  "build-artifact",
			Usage: "Upload/download artifacts from buildkite jobs",
			Subcommands: []cli.Command{
				{
					Name:        "download",
					Usage:       "Downloads artifacts from buildkite to the local machine.",
					Description: DownloadHelpDescription,
					Flags: []cli.Flag{
						// We don't default to $BUILDKITE_JOB_ID with --job because downloading artifacts should
						// default to all the jobs on the build, not just the current one. --job is used
						// to scope to a paticular job if you
						cli.StringFlag{
							Name:  "job",
							Value: "",
							Usage: "Used to target a specific job to download artifacts from",
						},
						cli.StringFlag{
							Name:   "build",
							Value:  "",
							Usage:  "Which build should the artifacts be downloaded from",
							EnvVar: "BUILDKITE_BUILD_ID",
						},
						cli.StringFlag{
							Name:   "agent-access-token",
							Value:  "",
							Usage:  "The access token used to identify the agent",
							EnvVar: "BUILDKITE_AGENT_ACCESS_TOKEN",
						},
						cli.StringFlag{
							Name:   "endpoint",
							Value:  "https://agent.buildkite.com/v2",
							Usage:  "The agent API endpoint",
							EnvVar: "BUILDKITE_AGENT_ENDPOINT",
						},
						cli.BoolFlag{
							Name:   "debug",
							Usage:  "Enable debug mode",
							EnvVar: "BUILDKITE_AGENT_DEBUG",
						},
					},
					Action: command.ArtifactDownloadCommandAction,
				},
				{
					Name:        "upload",
					Usage:       "Uploads files to a job as artifacts.",
					Description: UploadHelpDescription,
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:   "job",
							Value:  "",
							Usage:  "Which job should the artifacts be downloaded from",
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
							Value:  "https://agent.buildkite.com/v2",
							Usage:  "The agent API endpoint",
							EnvVar: "BUILDKITE_AGENT_ENDPOINT",
						},
						cli.BoolFlag{
							Name:   "debug",
							Usage:  "Enable debug mode",
							EnvVar: "BUILDKITE_AGENT_DEBUG",
						},
					},
					Action: command.ArtifactUploadCommandAction,
				},
			},
		},
		{
			Name:  "build-data",
			Usage: "Get/set data from buildkite jobs",
			Subcommands: []cli.Command{
				{
					Name:        "set",
					Usage:       "Set data on a build",
					Description: SetHelpDescription,
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:   "job",
							Value:  "",
							Usage:  "Which job should the artifacts be downloaded from",
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
							Value:  "https://agent.buildkite.com/v2",
							Usage:  "The agent API endpoint",
							EnvVar: "BUILDKITE_AGENT_ENDPOINT",
						},
						cli.BoolFlag{
							Name:   "debug",
							Usage:  "Enable debug mode",
							EnvVar: "BUILDKITE_AGENT_DEBUG",
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
							Usage:  "Which job should the data be retrieved from",
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
							Value:  "https://agent.buildkite.com/v2",
							Usage:  "The agent API endpoint",
							EnvVar: "BUILDKITE_AGENT_ENDPOINT",
						},
						cli.BoolFlag{
							Name:   "debug",
							Usage:  "Enable debug mode",
							EnvVar: "BUILDKITE_AGENT_DEBUG",
						},
					},
					Action: command.DataGetCommandAction,
				},
			},
		},
	}
}
