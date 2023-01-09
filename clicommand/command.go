package clicommand

import (
	"fmt"
	"os"

	"github.com/buildkite/agent/v3/cliconfig"
	"github.com/buildkite/agent/v3/logger"
	"github.com/urfave/cli"
)

type configType interface {
	AnnotateConfig | AgentStartConfig
}

type commandConfig[T configType] struct {
	cliContext   *cli.Context
	config       T
	logger       logger.Logger
	configLoader cliconfig.Loader
}

func newCommand[T configType](f func(cc commandConfig[T])) func(*cli.Context) {
	return func(c *cli.Context) {
		cfg := new(T)
		// The configuration will be loaded into this struct
		loader := cliconfig.Loader{
			CLI:                    c,
			Config:                 cfg,
			DefaultConfigFilePaths: DefaultConfigFilePaths(),
		}

		warnings, err := loader.Load()
		if err != nil {
			fmt.Printf("%s", err)
			os.Exit(1)
		}

		l := CreateLogger(cfg)

		// Now that we have a logger, log out the warnings that loading config generated
		for _, warning := range warnings {
			l.Warn("%s", warning)
		}

		// Setup any global configuration options
		done := HandleGlobalFlags(l, cfg)
		defer done()

		f(commandConfig[T]{
			cliContext:   c,
			config:       *cfg,
			logger:       l,
			configLoader: loader,
		})
	}
}
