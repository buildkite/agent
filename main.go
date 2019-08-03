package main

// see https://blog.golang.org/generate
//go:generate go run mime/generate.go
//go:generate go fmt mime/mime.go

import (
	"fmt"
	"os"

	"github.com/buildkite/agent/agent"
	"github.com/buildkite/agent/clicommand"
	"github.com/urfave/cli"
)

var AppHelpTemplate = `Usage:

  {{.Name}} <command> [arguments...]

Available commands are:

  {{range .Commands}}{{.Name}}{{with .ShortName}}, {{.}}{{end}}{{ "\t" }}{{.Usage}}
  {{end}}
Use "{{.Name}} <command> --help" for more information about a command.

`

var SubcommandHelpTemplate = `Usage:

  {{.Name}} {{if .VisibleFlags}}<command>{{end}} [arguments...]

Available commands are:

   {{range .Commands}}{{.Name}}{{with .ShortName}}, {{.}}{{end}}{{ "\t" }}{{.Usage}}
   {{end}}{{if .VisibleFlags}}
Options:

   {{range .VisibleFlags}}{{.}}
   {{end}}{{end}}
`

var CommandHelpTemplate = `{{.Description}}

Options:

   {{range .VisibleFlags}}{{.}}
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
		clicommand.AnnotateCommand,
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
				clicommand.MetaDataKeysCommand,
			},
		},
		{
			Name:  "pipeline",
			Usage: "Make changes to the pipeline of the currently running build",
			Subcommands: []cli.Command{
				clicommand.PipelineUploadCommand,
			},
		},
		{
			Name:  "step",
			Usage: "Make changes to a step (this includes any jobs that were created from the step)",
			Subcommands: []cli.Command{
				clicommand.StepUpdateCommand,
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

	if err := app.Run(os.Args); err != nil {
		fmt.Printf("%v\n", err)
		os.Exit(1)
	}
}
