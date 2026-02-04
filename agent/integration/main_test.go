package integration

import (
	"bufio"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/clicommand"
	"github.com/buildkite/agent/v3/version"
	"github.com/urfave/cli"
)

var WriteExecutableCommand = cli.Command{
	Name:  "write-exec",
	Usage: "Write STDIN to an executable file",
	Action: func(c *cli.Context) error {
		path := c.Args().First()

		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
		if err != nil {
			return err
		}
		defer f.Close() //nolint:errcheck // Best-effort cleanup - primary Close error is returned.

		_, err = io.Copy(f, bufio.NewReader(os.Stdin))
		if err != nil {
			return err
		}
		return f.Close()
	},
}

func TestMain(m *testing.M) {
	if len(os.Args) <= 1 || strings.HasPrefix(os.Args[1], "-test.") {
		os.Exit(m.Run())
	}

	app := cli.NewApp()
	app.Name = "buildkite-agent"
	app.Version = version.Version()
	app.Commands = []cli.Command{
		{
			Name: "env",
			Subcommands: []cli.Command{
				clicommand.EnvDumpCommand,
			},
		},
		WriteExecutableCommand,
	}

	if err := app.Run(os.Args); err != nil {
		os.Exit(clicommand.PrintMessageAndReturnExitCode(err))
	}
}
