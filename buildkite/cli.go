package buildkite

import (
	"github.com/buildkite/agent/logger"
	"github.com/codegangsta/cli"
)

type CLI struct {
	// The cli context for this command
	Context *cli.Context
}

func (c CLI) Setup() CLI {
	if c.Context.Bool("debug") {
		logger.SetLevel(logger.DEBUG)
	}

	if c.Context.Bool("no-color") {
		logger.SetColors(false)
	}

	return c
}

func (c CLI) Require(keys ...string) {
	for _, k := range keys {
		if c.Context.String(k) == "" {
			logger.Fatal("Missing %s. See: `%s %s`", k, c.Context.App.Name, c.Context.Command.Name)
		}
	}
}

func (c CLI) RequireArgs(names ...string) {
	argsLength := len(c.Context.Args())

	if argsLength < len(names) {
		missing := names[argsLength]
		logger.Fatal("Missing %s argument. See: `%s %s`", missing, c.Context.App.Name, c.Context.Command.Name)
	}
}
