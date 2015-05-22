package buildkite

import (
	"github.com/buildkite/agent/buildkite/logger"
	"github.com/codegangsta/cli"
)

type CLI struct {
	// The cli context for this command
	Context *cli.Context
}

func (cli *CLI) Setup() {
	if cli.Context.Bool("debug") {
		logger.SetLevel(logger.DEBUG)
	}

	if cli.Context.Bool("no-color") {
		logger.SetColors(false)
	}
}

func (cli *CLI) Require(keys ...string) {
	for _, k := range keys {
		if cli.Context.String(k) == "" {
			logger.Fatal("Missing %s. See: `%s %s`", k, cli.Context.App.Name, cli.Context.Command.Name)
		}
	}
}
