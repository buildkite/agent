package hook_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/clicommand"
	"github.com/buildkite/agent/v3/version"
	"github.com/urfave/cli/v3"
)

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
		},
	}

	if err := app.Run(context.Background(), os.Args); err != nil {
		os.Exit(clicommand.PrintMessageAndReturnExitCode(err))
	}
}
