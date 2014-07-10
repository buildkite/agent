package main

import (
	"fmt"
	"github.com/buildboxhq/buildbox-agent/buildbox"
	"github.com/codegangsta/cli"
	"os"
)

var AppHelpTemplate = `A utility to upload/download artifacts for jobs on Buildbox

Usage:

  {{.Name}} command [arguments]

The commands are:

  {{range .Commands}}{{.Name}}{{with .ShortName}}, {{.}}{{end}}{{ "\t" }}{{.Usage}}
  {{end}}
Use "buildbox-artifact help [command]" for more information about a command.

`

var CommandHelpTemplate = `Usage: buildbox-artifact {{.Name}} [command options] [arguments...]

{{.Description}}

Options:
   {{range .Flags}}{{.}}
   {{end}}
`

var UploadHelpDescription = `Uploads files to a job as artifacts.

You need to ensure that the paths are surrounded by quotes otherwise the
built-in shell path globbing will provide the files, which is currently not
supported.

Example:

buildbox-artifact upload "log/**/*.log" --job [job] \
                                        --agent-access-token [agent-access-token]

You can also upload directy to Amazon S3 if you'd like to host your own artifacts:

export AWS_SECRET_ACCESS_KEY=yyy
export AWS_ACCESS_KEY_ID=xxx
buildbox-artifact upload "log/**/*.log" s3://name-of-your-s3-bucket/$BUILDBOX_JOB_ID --job [job] \
                                                                                     --agent-access-token [agent-access-token]`

var DownloadHelpDescription = `Downloads artifacts from Buildbox to the local machine.

You need to ensure that your search query is surrounded by quotes if
using a wild card as the built-in shell path globbing will provide files,
which will break the download.

Example:

buildbox-artifact download "pkg/*.tar.gz" . --build [build] \
                                            --agent-access-token [agent-access-token]

This will search across all the artifacts for the build with files that match that part.
The first argument is the search query, and the second argument is the download destination.

If you're trying to download a specific file, and there are multiple artifacts from different
jobs, you can target the paticular job you want to download the artifact from:

buildbox-artifact download "pkg/*.tar.gz" . --job "tests" \
                                            --build [build] \
                                            --agent-access-token [agent-access-token]

You can also use the job's id (provided by the environment variable $BUILDBOX_JOB_ID)`

var JobIdEnv = "BUILDBOX_JOB_ID"
var JobIdDefault = "$" + JobIdEnv

var BuildIdEnv = "BUILDBOX_BUILD_ID"
var BuildIdDefault = "$" + BuildIdEnv

var AgentAccessTokenEnv = "BUILDBOX_AGENT_ACCESS_TOKEN"
var AgentAccessTokenDefault = "$" + AgentAccessTokenEnv

func main() {
	cli.AppHelpTemplate = AppHelpTemplate
	cli.CommandHelpTemplate = CommandHelpTemplate

	app := cli.NewApp()
	app.Name = "buildbox-artifact"
	app.Version = buildbox.Version

	// Define the actions for our CLI
	app.Commands = []cli.Command{
		{
			Name:        "upload",
			Usage:       "Upload the following artifacts to the build",
			Description: UploadHelpDescription,
			Flags: []cli.Flag{
				cli.StringFlag{"job", JobIdDefault, "Which job should the artifacts be uploaded to"},
				cli.StringFlag{"agent-access-token", AgentAccessTokenDefault, "The access token used to identify the agent"},
				cli.StringFlag{"url", "https://agent.buildbox.io/v1", "The agent API endpoint"},
				cli.BoolFlag{"debug", "Enable debug mode"},
			},
			Action: func(c *cli.Context) {
				// Init debugging
				if c.Bool("debug") {
					buildbox.LoggerInitDebug()
				}

				agentAccessToken := c.String("agent-access-token")

				// Should we look to the environment for the agent access token?
				if agentAccessToken == AgentAccessTokenDefault {
					agentAccessToken = os.Getenv(AgentAccessTokenEnv)
				}

				if agentAccessToken == "" {
					fmt.Printf("%s: missing agent access token\nSee '%s help upload'\n", app.Name, app.Name)
					os.Exit(1)
				}

				jobId := c.String("job")

				// Should we look to the environment for the job id?
				if jobId == JobIdDefault {
					jobId = os.Getenv(JobIdEnv)
				}

				if jobId == "" {
					fmt.Printf("%s: missing job\nSee '%s help upload'\n", app.Name, app.Name)
					os.Exit(1)
				}

				// Grab the first argument and use as paths to download
				paths := c.Args().First()
				if paths == "" {
					fmt.Printf("%s: missing upload paths\nSee '%s help upload'\n", app.Name, app.Name)
					os.Exit(1)
				}

				// Do we have a custom destination
				destination := ""
				if len(c.Args()) > 1 {
					destination = c.Args()[1]
				}

				// Set the agent options
				var agent buildbox.Agent

				// Client specific options
				agent.Client.AgentAccessToken = agentAccessToken
				agent.Client.URL = c.String("url")

				// Setup the agent
				agent.Setup()

				// Find the actual job now
				job, err := agent.Client.JobFind(jobId)
				if err != nil {
					buildbox.Logger.Fatalf("Could not find job: %s", jobId)
				}

				// Create artifact structs for all the files we need to upload
				artifacts, err := buildbox.CollectArtifacts(job, paths)
				if err != nil {
					buildbox.Logger.Fatalf("Failed to collect artifacts: %s", err)
				}

				if len(artifacts) == 0 {
					buildbox.Logger.Infof("No files matched paths: %s", paths)
				} else {
					buildbox.Logger.Infof("Found %d files that match \"%s\"", len(artifacts), paths)

					err := buildbox.UploadArtifacts(agent.Client, job, artifacts, destination)
					if err != nil {
						buildbox.Logger.Fatalf("Failed to upload artifacts: %s", err)
					}
				}
			},
		},
		{
			Name:        "download",
			Usage:       "Download the following artifacts",
			Description: DownloadHelpDescription,
			Flags: []cli.Flag{
				cli.StringFlag{"job", "", "Which job should the artifacts be downloaded from"},
				cli.StringFlag{"build", BuildIdDefault, "Which build should the artifacts be downloaded from"},
				cli.StringFlag{"agent-access-token", AgentAccessTokenDefault, "The access token used to identify the agent"},
				cli.StringFlag{"url", "https://agent.buildbox.io/v1", "The agent API endpoint"},
				cli.BoolFlag{"debug", "Enable debug mode"},
			},
			Action: func(c *cli.Context) {
				// Init debugging
				if c.Bool("debug") {
					buildbox.LoggerInitDebug()
				}

				agentAccessToken := c.String("agent-access-token")

				// Should we look to the environment for the agent access token?
				if agentAccessToken == AgentAccessTokenDefault {
					agentAccessToken = os.Getenv(AgentAccessTokenEnv)
				}

				if agentAccessToken == "" {
					fmt.Printf("%s: missing agent access token\nSee '%s help download'\n", app.Name, app.Name)
					os.Exit(1)
				}

				if len(c.Args()) != 2 {
					fmt.Printf("%s: invalid usage\nSee '%s help download'\n", app.Name, app.Name)
					os.Exit(1)
				}

				// Find the build id
				buildId := c.String("build")
				if buildId == BuildIdDefault {
					buildId = os.Getenv(BuildIdEnv)
				}

				if buildId == "" {
					fmt.Printf("%s: missing build\nSee '%s help download'\n", app.Name, app.Name)
					os.Exit(1)
				}

				// Get our search query and download destination
				searchQuery := c.Args()[0]
				downloadDestination := c.Args()[1]
				jobQuery := c.String("job")

				// Set the agent options
				var agent buildbox.Agent

				// Client specific options
				agent.Client.AgentAccessToken = agentAccessToken
				agent.Client.URL = c.String("url")

				// Setup the agent
				agent.Setup()

				if jobQuery == "" {
					buildbox.Logger.Infof("Searching for artifacts: \"%s\"", searchQuery)
				} else {
					buildbox.Logger.Infof("Searching for artifacts: \"%s\" within job: \"%s\"", searchQuery, jobQuery)
				}

				fmt.Printf("query: %s\n", searchQuery)
				fmt.Printf("job: %s\n", jobQuery)
				fmt.Printf("destination: %s\n", downloadDestination)

				// Search for artifacts to download
				artifacts, err := agent.Client.SearchArtifacts(buildId, searchQuery, jobQuery)
				if err != nil {
					buildbox.Logger.Fatalf("Failed to find artifacts: %s", err)
				}

				buildbox.Logger.Debugf("%s", artifacts)
			},
		},
	}

	// Default the default action
	app.Action = func(c *cli.Context) {
		cli.ShowAppHelp(c)
		os.Exit(1)
	}

	app.Run(os.Args)
}
