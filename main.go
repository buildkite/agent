// Buildkite-agent is a small, reliable, cross-platform build runner that makes
// it easy to run automated builds on your own infrastructure.
package main

// see https://blog.golang.org/generate
//go:generate go run mime/generate.go
//go:generate go fmt mime/mime.go

import (
	"fmt"
	"os"

	"github.com/buildkite/agent/v3/clicommand"
	"github.com/buildkite/agent/v3/version"
	"github.com/urfave/cli"
)

const appHelpTemplate = `Usage:

  {{.Name}} <command> [options...]

Available commands are:

  {{range .Commands}}{{.Name}}{{with .ShortName}}, {{.}}{{end}}{{ "\t" }}{{.Usage}}
  {{end}}
Use "{{.Name}} <command> --help" for more information about a command.

`

const subcommandHelpTemplate = `Usage:

  {{.Name}} {{if .VisibleFlags}}<command>{{end}} [options...]

Available commands are:

   {{range .Commands}}{{.Name}}{{with .ShortName}}, {{.}}{{end}}{{ "\t" }}{{.Usage}}
   {{end}}{{if .VisibleFlags}}
Options:

   {{range .VisibleFlags}}{{.}}
   {{end}}{{end}}
`

const commandHelpTemplate = `{{.Description}}

Options:

   {{range .VisibleFlags}}{{.}}
   {{end}}
`

func printVersion(c *cli.Context) {
	fmt.Printf("%v version %v, build %v\n", c.App.Name, c.App.Version, version.BuildVersion())
}

func main() {
	cli.AppHelpTemplate = appHelpTemplate
	cli.CommandHelpTemplate = commandHelpTemplate
	cli.SubcommandHelpTemplate = subcommandHelpTemplate
	cli.VersionPrinter = printVersion

	app := cli.NewApp()
	app.Name = "buildkite-agent"
	app.Version = version.Version()
	app.Commands = []cli.Command{
		clicommand.AcknowledgementsCommand,
		clicommand.AgentStartCommand,
		clicommand.AnnotateCommand,
		{
			Name:  "annotation",
			Usage: "Make changes an annotation on the currently running build",
			Subcommands: []cli.Command{
				clicommand.AnnotationRemoveCommand,
			},
		},
		{
			Name:  "artifact",
			Usage: "Upload/download artifacts from Buildkite jobs",
			Subcommands: []cli.Command{
				clicommand.ArtifactUploadCommand,
				clicommand.ArtifactDownloadCommand,
				clicommand.ArtifactSearchCommand,
				clicommand.ArtifactShasumCommand,
			},
		},
		{
			Name:  "env",
			Usage: "Process environment subcommands",
			Subcommands: []cli.Command{
				clicommand.EnvDumpCommand,
				clicommand.EnvGetCommand,
				clicommand.EnvSetCommand,
				clicommand.EnvUnsetCommand,
			},
		},
		{
			Name:  "lock",
			Usage: "Process lock subcommands",
			Subcommands: []cli.Command{
				clicommand.LockAcquireCommand,
				clicommand.LockDoCommand,
				clicommand.LockDoneCommand,
				clicommand.LockGetCommand,
				clicommand.LockReleaseCommand,
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
			Name:  "oidc",
			Usage: "Interact with Buildkite OpenID Connect (OIDC)",
			Subcommands: []cli.Command{
				clicommand.OIDCRequestTokenCommand,
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
			Usage: "Get or update an attribute of a build step",
			Subcommands: []cli.Command{
				clicommand.StepGetCommand,
				clicommand.StepUpdateCommand,
			},
		},
		clicommand.BootstrapCommand,
		{
			Name:  "job",
			Usage: "Interact with Buildkite jobs",
			Subcommands: []cli.Command{
				clicommand.JobRunCommand,
			},
		},
	}

	app.ErrWriter = os.Stderr

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
