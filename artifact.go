package main

import (
  "os"
  "fmt"
  "log"
  "github.com/codegangsta/cli"
  "github.com/buildboxhq/buildbox-agent/buildbox"
)

var AppHelpTemplate = `A utility to upload/download artifacts for jobs on Buildbox

Usage:

  {{.Name}} command [arguments]

The comamnds are:

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

buildbox-artifact upload "log/**/*.log" --job [job] \
                                        --agent-access-token [agent-access-token] \
                                        --aws-secret-access-key ...\
                                        --aws-access-id ...\
                                        --destination s3://bucket-name/foo/bar`

var JobIdEnv = "BUILDBOX_JOB_ID"
var JobIdDefault = "$" + JobIdEnv
var AgentAccessTokenEnv = "BUILDBOX_AGENT_ACCESS_TOKEN"
var AgentAccessTokenDefault = "$" + AgentAccessTokenEnv

func main() {
  cli.AppHelpTemplate = AppHelpTemplate
  cli.CommandHelpTemplate = CommandHelpTemplate

  app := cli.NewApp()
  app.Name = "buildbox-artifact"
  app.Version = "0.1.alpha"

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

        // Set the agent options
        var agent buildbox.Agent;
        agent.Debug = c.Bool("debug")

        // Client specific options
        agent.Client.AgentAccessToken = agentAccessToken
        agent.Client.URL = c.String("url")
        agent.Client.Debug = agent.Debug

        // Tell the user that debug mode has been enabled
        if agent.Debug {
          log.Printf("Debug mode enabled")
        }

        // Setup the agent
        agent.Setup()

        // Find the actual job now
        job, err := agent.Client.JobFind(jobId)
        if err != nil {
          log.Fatalf("Could not find job: %s", jobId)
        }

        // Create artifact structs for all the files we need to upload
        artifacts, err := buildbox.CollectArtifacts(job, paths)
        if err != nil {
          log.Fatalf("Failed to collect artifacts: %s", err)
        }

        if len(artifacts) == 0 {
          log.Print("No files matched paths: %s", paths)
        } else {
          log.Printf("Preparting to upload %d file(s)", len(artifacts))

          artifacts, err := buildbox.CreateArtifacts(agent.Client, job, artifacts)
          if err != nil {
            log.Fatalf("Failed to prepare artifacts: %s", err)
          }
          for x, artifact := range artifacts {
            log.Printf("%d %s", x, artifact)
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
