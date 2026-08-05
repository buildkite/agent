package integration

import (
	"bufio"
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/buildkite/agent/v4/clicommand"
	"github.com/buildkite/agent/v4/version"
	"github.com/urfave/cli/v3"
)

var WriteExecutableCommand = &cli.Command{
	Name:  "write-exec",
	Usage: "Write STDIN to an executable file",
	Action: func(ctx context.Context, c *cli.Command) error {
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

	app := &cli.Command{
		Name:    "buildkite-agent",
		Version: version.Version(),
		Commands: []*cli.Command{
			{
				Name: "env",
				Commands: []*cli.Command{
					clicommand.EnvDumpCommand,
				},
			},
			WriteExecutableCommand,
		},
	}

	if err := app.Run(context.Background(), os.Args); err != nil {
		os.Exit(clicommand.PrintMessageAndReturnExitCode(err))
	}
}
