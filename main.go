// Buildkite-agent is a small, reliable, cross-platform build runner that makes
// it easy to run automated builds on your own infrastructure.
package main

// see https://blog.golang.org/generate
//go:generate go run internal/mime/generate.go
//go:generate go tool gofumpt -w internal/mime/mime.go

import (
	"fmt"
	"os"

	"github.com/buildkite/agent/v3/clicommand"
	"github.com/buildkite/agent/v3/version"
	"github.com/urfave/cli"
)

const appHelpTemplate = `Usage:
  {{.Name}} <command> [options...]

Available commands are: {{range .VisibleCategories}}{{if .Name}}
{{.Name}}:{{range .VisibleCommands}}
  {{join .Names ", "}}{{"\t"}}{{.Usage}}{{end}}{{"\n"}}{{else}}{{range .VisibleCommands}}
  {{join .Names ", "}}{{"\t"}}{{.Usage}}{{end}}{{"\n"}}{{end}}{{end}}
Use "{{.Name}} <command> --help" for more information about a command.
`

const subcommandHelpTemplate = `Usage:

  {{.Name}} {{if .VisibleFlags}}<command>{{end}} [options...]

Available commands are:

  {{range .Commands}}{{.Name}}{{with .ShortName}}, {{.}}{{end}}{{ "\t" }}{{.Usage}}
  {{end}}{{if .VisibleFlags}}

Options:

{{range .VisibleFlags}}  {{.}}
{{end}}{{ end -}}
`

const commandHelpTemplate = `{{.Description}}

Options:

{{range .VisibleFlags}}  {{.}}
{{ end -}}
`

func printVersion(c *cli.Context) {
	fmt.Fprintf(c.App.Writer, "%s version %s\n", c.App.Name, version.FullVersion())
}

func main() {
	cli.AppHelpTemplate = appHelpTemplate
	cli.CommandHelpTemplate = commandHelpTemplate
	cli.SubcommandHelpTemplate = subcommandHelpTemplate
	cli.VersionPrinter = printVersion

	app := cli.NewApp()
	app.Name = "buildkite-agent"
	app.Version = version.Version()
	app.Commands = clicommand.BuildkiteAgentCommands
	app.ErrWriter = os.Stderr

	// When a sub command can't be found
	app.CommandNotFound = func(c *cli.Context, command string) {
		fmt.Fprintf(app.ErrWriter, "buildkite-agent: unknown subcommand %q\n", command)
		fmt.Fprintf(app.ErrWriter, "Run '%s --help' for usage.\n", c.App.Name)
		os.Exit(1)
	}

	if err := app.Run(os.Args); err != nil {
		os.Exit(clicommand.PrintMessageAndReturnExitCode(err))
	}
}
