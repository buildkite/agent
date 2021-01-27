package integration

import (
	"fmt"
	"os"
	"testing"

	"github.com/buildkite/agent/agent"
	"github.com/buildkite/agent/clicommand"
	"github.com/buildkite/bintest"
	"github.com/urfave/cli"
)

func TestMain(m *testing.M) {
	// If we are passed "bootstrap", execute like the bootstrap cli
	if len(os.Args) > 1 && os.Args[1] == `bootstrap` {
		app := cli.NewApp()
		app.Name = "buildkite-agent"
		app.Version = agent.Version()
		app.Commands = []cli.Command{
			clicommand.BootstrapCommand,
		}

		if err := app.Run(os.Args); err != nil {
			fmt.Printf("%v\n", err)
			os.Exit(1)
		}

		os.Exit(0)
	}

	if os.Getenv(`BINTEST_DEBUG`) == "1" {
		bintest.Debug = true
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
