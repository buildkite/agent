package main

import (
  "os"
  "github.com/codegangsta/cli"
  "github.com/buildboxhq/buildbox-agent-go/buildbox"
)

/*
func start(cli *Cli) {
}
*/

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

buildbox-agent start --access-token a374fha7834f \
                     --bootstrap-script ~/.buildbox/bootstrap.sh`

func main() {
  cli.AppHelpTemplate = AppHelpTemplate
  cli.CommandHelpTemplate = CommandHelpTemplate

  app := cli.NewApp()
  app.Name = "buildbox.agent"
  app.Version = buildbox.Version

  // Define the actions for our CLI
  app.Commands = []cli.Command{
    {
      Name:  "start",
      Usage: "Starts the Buildbox agent",
      Description: StartHelpDescription,
      Flags: []cli.Flag {
        cli.StringFlag{"access-token", "", "The access token used to identify the agent."},
        cli.StringFlag{"bootstrap-script", "bootstrap.sh", "Path to the bootstrap script."},
        cli.StringFlag{"url", "https://agent.buildbox.io/v1", "The Agent API endpoint."},
        cli.BoolFlag{"exit-on-complete", "Runs all available jobs and then exit."},
        cli.BoolFlag{"debug", "Enable debug mode."},
      },
      Action: func(c *cli.Context) {
        if c.String("access-token") == "" {
          print("buildbox-agent: missing access token\nSee 'buildbox-agent help start'\n")
          os.Exit(1)
        }

        // Set the agent options
        var agent buildbox.Agent;
        agent.Debug = c.Bool("debug")
        agent.ExitOnComplete = c.Bool("exit-on-complete")
        agent.BootstrapScript = c.String("bootstrap-script")

        // Client specific options
        agent.Client.URL = c.String("url")

        // Run the agent
        agent.Run()
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
