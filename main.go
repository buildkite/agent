package main

import (
	"fmt"
	"os"

	"github.com/buildkite/agent/agent"
	"github.com/buildkite/agent/clicommand"
	"github.com/codegangsta/cli"
)

var AppHelpTemplate = `Usage:

  {{.Name}} <command> [arguments...]

Available commands are:

  {{range .Commands}}{{.Name}}{{with .ShortName}}, {{.}}{{end}}{{ "\t" }}{{.Usage}}
  {{end}}
Use "{{.Name}} <command> --help" for more information about a command.

`

var SubcommandHelpTemplate = `Usage:

  {{.Name}} {{if .Flags}}<command>{{end}} [arguments...]

Available commands are:

   {{range .Commands}}{{.Name}}{{with .ShortName}}, {{.}}{{end}}{{ "\t" }}{{.Usage}}
   {{end}}{{if .Flags}}
Options:

   {{range .Flags}}{{.}}
   {{end}}{{end}}
`

var CommandHelpTemplate = `{{.Description}}

Options:

   {{range .Flags}}{{.}}
   {{end}}
`

func printVersion(c *cli.Context) {
	fmt.Printf("%v version %v, build %v\n", c.App.Name, c.App.Version, agent.BuildVersion())
}

func main() {
	cli.AppHelpTemplate = AppHelpTemplate
	cli.CommandHelpTemplate = CommandHelpTemplate
	cli.SubcommandHelpTemplate = SubcommandHelpTemplate
	cli.VersionPrinter = printVersion

	app := cli.NewApp()
	app.Name = "buildkite-agent"
	app.Version = agent.Version()
	app.Commands = []cli.Command{
		clicommand.AgentStartCommand,
		{
			Name:  "artifact",
			Usage: "Upload/download artifacts from Buildkite jobs",
			Subcommands: []cli.Command{
				clicommand.ArtifactUploadCommand,
				clicommand.ArtifactDownloadCommand,
				clicommand.ArtifactShasumCommand,
			},
		},
		{
			Name:  "meta-data",
			Usage: "Get/set data from Buildkite jobs",
			Subcommands: []cli.Command{
				clicommand.MetaDataSetCommand,
				clicommand.MetaDataGetCommand,
				clicommand.MetaDataExistsCommand,
			},
		},
		{
			Name:  "pipeline",
			Usage: "Make changes to the pipeline of the currently running build",
			Subcommands: []cli.Command{
				clicommand.PipelineUploadCommand,
				clicommand.PipelineValidateCommand,
			},
		},
		clicommand.BootstrapCommand,
	}

	// When no sub command is used
	app.Action = func(c *cli.Context) {
		cli.ShowAppHelp(c)
		os.Exit(1)
	}

	// When a sub command can't be found
	app.CommandNotFound = func(c *cli.Context, command string) {
		cli.ShowAppHelp(c)
		os.Exit(1)
	}

	app.Run(os.Args)
}
