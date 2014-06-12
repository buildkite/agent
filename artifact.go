package main

import (
  "os"
  "fmt"
  "github.com/codegangsta/cli"
  "github.com/buildboxhq/buildbox-agent/buildbox"
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

var JobIdEnv = "BUILDBOX_JOB_ID"
var JobIdDefault = "$" + JobIdEnv
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
      Name:  "upload",
      Usage: "Upload the following artifacts to the build",
      Description: UploadHelpDescription,
      Flags: []cli.Flag {
        cli.StringFlag{"job", JobIdDefault, "Which job should the artifacts be uploaded to"},
        cli.StringFlag{"agent-access-token", AgentAccessTokenDefault, "The access token used to identify the agent"},
        cli.StringFlag{"url", "https://agent.buildbox.io/v1", "The agent API endpoint"},
        cli.BoolFlag{"debug", "Enable debug mode"},
      },
      Action: func(c *cli.Context) {
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
        var agent buildbox.Agent;
        agent.Debug = c.Bool("debug")

        // Client specific options
        agent.Client.AgentAccessToken = agentAccessToken
        agent.Client.URL = c.String("url")
        agent.Client.Debug = agent.Debug

        // Tell the user that debug mode has been enabled
        // TODO: Enable debug

        // Setup the agent
        agent.Setup()

        // Find the actual job now
        job, err := agent.Client.JobFind(jobId)
        if err != nil {
          buildbox.Logger.Fatal("Could not find job: %s", jobId)
        }

        // Create artifact structs for all the files we need to upload
        artifacts, err := buildbox.CollectArtifacts(job, paths)
        if err != nil {
          buildbox.Logger.Fatal("Failed to collect artifacts: %s", err)
        }

        if len(artifacts) == 0 {
          buildbox.Logger.Info("No files matched paths: %s", paths)
        } else {
          buildbox.Logger.Info("Uploading %d files that match \"%s\"", len(artifacts), paths)

          err := buildbox.UploadArtifacts(agent.Client, job, artifacts, destination)
          if err != nil {
            buildbox.Logger.Fatal("Failed to upload artifacts: %s", err)
          }
        }
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
