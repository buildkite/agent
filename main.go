package main

import (
  "os"
  "path"
  "fmt"
  "github.com/codegangsta/cli"
  "github.com/buildboxhq/agent-go/buildbox"
)

var AppHelpTemplate = `The agent performs builds and sends the results back to Buildbox.

Usage:

  {{.Name}} command [arguments]

The comamnds are:

  {{range .Commands}}{{.Name}}{{with .ShortName}}, {{.}}{{end}}{{ "\t" }}{{.Usage}}
  {{end}}
Use "buildbox-agent help [command]" for more information about a command.

`

var CommandHelpTemplate = `Usage: buildbox-agent {{.Name}} [command options] [arguments...]

{{.Description}}

Options:
   {{range .Flags}}{{.}}
   {{end}}
`

var StartHelpDescription = `When a job is ready to run it will call the "bootstrap-script"
and pass it all the environment variables required for the job to run.
This script is responsible for checking out the code, and running the
actual build script defined in the project.

The agent will run any jobs within a PTY (Pseudo terminal) if available.

Example:

buildbox-agent start --access-token [access-token] \
                     --bootstrap-script ~/.buildbox/bootstrap.sh`

var RunHelpDescription = `Manually run a scheduled job. If the job has already assigned
to another agent, it will not run.

Example:

buildbox-agent run [job-id] --access-token [access-token]`

var BootstrapScriptDefault = "$HOME/.buildbox/bootstrap.sh"

func main() {
  cli.AppHelpTemplate = AppHelpTemplate
  cli.CommandHelpTemplate = CommandHelpTemplate

  app := cli.NewApp()
  app.Name = "buildbox-agent"
  app.Version = buildbox.Version

  // Define the actions for our CLI
  app.Commands = []cli.Command{
    {
      Name:  "start",
      Usage: "Starts the Buildbox agent",
      Description: StartHelpDescription,
      Flags: []cli.Flag {
        cli.StringFlag{"access-token", "", "The access token used to identify the agent."},
        cli.StringFlag{"bootstrap-script", BootstrapScriptDefault, "Path to the bootstrap script."},
        cli.StringFlag{"url", "https://agent.buildbox.io/v1", "The Agent API endpoint."},
        cli.BoolFlag{"debug", "Enable debug mode."},
      },
      Action: func(c *cli.Context) {
        // Create the agent from the CLI options
        agent := setupAgentFromCli(c, "start")

        // Start the agent
        agent.Start()
      },
    },
    {
      Name:  "run",
      Usage: "Manually run a scheduled job",
      Description: RunHelpDescription,
      Flags: []cli.Flag {
        cli.StringFlag{"access-token", "", "The access token used to identify the agent."},
        cli.StringFlag{"bootstrap-script", BootstrapScriptDefault, "Path to the bootstrap script."},
        cli.StringFlag{"url", "https://agent.buildbox.io/v1", "The Agent API endpoint."},
        cli.BoolFlag{"debug", "Enable debug mode."},
      },
      Action: func(c *cli.Context) {
        // Create the agent from the CLI options
        agent := setupAgentFromCli(c, "run")

        // Grab the first argument and use as the job id
        id := c.Args().First()

        // Validate the job id
        if id == "" {
          fmt.Printf("buildbox-agent: no job id specified.\nSee 'buildbox-agent help run'")
          os.Exit(1)
        }

        agent.Run(id)
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

func setupAgentFromCli(c *cli.Context, command string) buildbox.Agent {
  if c.String("access-token") == "" {
    fmt.Printf("buildbox-agent: missing access token\nSee 'buildbox-agent help %s'\n", command)
    os.Exit(1)
  }

  bootstrapScript := c.String("bootstrap-script")

  // Go doesn't provide an a mechanism to turn $HOME into an absolute
  // path, so if they're using the default, I'll remake it using real
  // values.
  if c.String("bootstrap-script") == BootstrapScriptDefault {
    homeDir := os.Getenv("HOME")

    bootstrapScript = path.Join(homeDir, ".buildbox", "bootstrap.sh")
  }

  // Make sure the boostrap script exists.
  if _, err := os.Stat(bootstrapScript); os.IsNotExist(err) {
    print("buildbox-agent: no such file " + bootstrapScript + "\n")
    os.Exit(1)
  }

  // Set the agent options
  var agent buildbox.Agent;
  agent.Debug = c.Bool("debug")
  agent.BootstrapScript = bootstrapScript

  // Client specific options
  agent.Client.AgentAccessToken = c.String("access-token")
  agent.Client.URL = c.String("url")
  agent.Client.Debug = agent.Debug

  // Setup the agent
  agent.Setup()

  return agent
}
