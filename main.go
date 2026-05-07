// Buildkite-agent is a small, reliable, cross-platform build runner that makes
// it easy to run automated builds on your own infrastructure.
package main

// see https://blog.golang.org/generate
//go:generate go run internal/mime/generate.go
//go:generate go tool gofumpt -w internal/mime/mime.go

import (
	"context"
	"fmt"
	"os"

	"github.com/buildkite/agent/v4/clicommand"
	"github.com/buildkite/agent/v4/version"
	"github.com/urfave/cli/v3"
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

  {{range .Commands}}{{.Name}}{{range .Aliases}}, {{.}}{{end}}{{ "\t" }}{{.Usage}}
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

func printVersion(c *cli.Command) {
	_, _ = fmt.Fprintf(c.Writer, "%s version %s\n", c.Root().Name, version.FullVersion())
}

func main() {
	cli.CommandHelpTemplate = commandHelpTemplate
	cli.SubcommandHelpTemplate = subcommandHelpTemplate
	cli.VersionPrinter = printVersion

	app := &cli.Command{
		Name:                          "buildkite-agent",
		Version:                       version.Version(),
		Commands:                      clicommand.BuildkiteAgentCommands,
		CustomRootCommandHelpTemplate: appHelpTemplate,
	}

	// When a sub command can't be found
	app.CommandNotFound = func(ctx context.Context, c *cli.Command, command string) {
		_, _ = fmt.Fprintf(app.ErrWriter, "buildkite-agent: unknown subcommand %q\n", command)
		_, _ = fmt.Fprintf(app.ErrWriter, "Run '%s --help' for usage.\n", c.Root().Name)
		os.Exit(1)
	}

	if err := app.Run(context.Background(), os.Args); err != nil {
		os.Exit(clicommand.PrintMessageAndReturnExitCode(err))
	}
}
