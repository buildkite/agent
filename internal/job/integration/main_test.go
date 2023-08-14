package integration

import (
	"context"
	"fmt"
	"os"
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
	// If we are passed "bootstrap", execute like the bootstrap cli
	if len(os.Args) > 1 && os.Args[1] == "bootstrap" {
		app := cli.NewApp()
		app.Name = "buildkite-agent"
		app.Version = version.Version()
		app.Commands = []cli.Command{
			clicommand.BootstrapCommand,
		}

		if err := app.Run(os.Args); err != nil {
			os.Exit(clicommand.ErrToExitCode(err))
		}

		return
	}

	if os.Getenv("BINTEST_DEBUG") == "1" {
		bintest.Debug = true
	}

	// Support running the test suite against a given experiment
	if exp := os.Getenv("TEST_EXPERIMENT"); exp != "" {
		mainCtx, _ = experiments.Enable(mainCtx, exp)
		fmt.Printf("!!! Enabling experiment %q for test suite\n", exp)
	}

	// Start a server to share
	_, err := bintest.StartServer()
	if err != nil {
		fmt.Printf("Error starting server: %v", err)
		os.Exit(1)
	}

	code := m.Run()
	os.Exit(code)
}
