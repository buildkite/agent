package integration

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/clicommand"
	"github.com/buildkite/agent/v3/internal/experiments"
	"github.com/buildkite/agent/v3/version"
	"github.com/buildkite/bintest/v3"
	"github.com/urfave/cli"
)

// This context needs to be stored here in order to pass experiments to tests,
// because testing.M can only Run (without passing arguments or context).
// In ordinary code, pass contexts as arguments!
var mainCtx = context.Background()

func TestMain(m *testing.M) {
	if len(os.Args) <= 1 || strings.HasPrefix(os.Args[1], "-test.") {
		if os.Getenv("BINTEST_DEBUG") == "1" {
			bintest.Debug = true
		}

		// Support running the test suite against a given experiment
		if exp := os.Getenv("TEST_EXPERIMENT"); exp != "" {
			mainCtx, _ = experiments.Enable(mainCtx, exp)
			fmt.Fprintf(os.Stderr, "!!! Enabling experiment %q for test suite\n", exp)
		}

		// Start a server to share
		if _, err := bintest.StartServer(); err != nil {
			fmt.Fprintf(os.Stderr, "Error starting server: %v", err)
			os.Exit(1)
		}

		os.Exit(m.Run())
	}

	app := cli.NewApp()
	app.Name = "buildkite-agent"
	app.Version = version.Version()
	app.Commands = []cli.Command{
		clicommand.BootstrapCommand,
		{
			Name: "env",
			Subcommands: []cli.Command{
				clicommand.EnvDumpCommand,
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		os.Exit(clicommand.PrintMessageAndReturnExitCode(err))
	}
}
