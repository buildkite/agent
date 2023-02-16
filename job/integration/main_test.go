package integration

import (
	"fmt"
	"os"
	"testing"

	"github.com/buildkite/agent/v3/clicommand"
	"github.com/buildkite/agent/v3/experiments"
	"github.com/buildkite/agent/v3/version"
	"github.com/buildkite/bintest/v3"
	"github.com/urfave/cli"
)

func TestMain(m *testing.M) {
	// If we are passed "exec-job", execute like the exec-job cli
	if len(os.Args) > 1 && os.Args[1] == "exec-job" {
		app := cli.NewApp()
		app.Name = "buildkite-agent"
		app.Version = version.Version()
		app.Commands = []cli.Command{
			clicommand.ExecJobCommand,
		}

		if err := app.Run(os.Args); err != nil {
			fmt.Printf("%v\n", err)
			os.Exit(1)
		}

		os.Exit(0)
	}

	if os.Getenv("BINTEST_DEBUG") == "1" {
		bintest.Debug = true
	}

	// Support running the test suite against a given experiment
	if exp := os.Getenv("TEST_EXPERIMENT"); exp != "" {
		experiments.Enable(exp)
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
