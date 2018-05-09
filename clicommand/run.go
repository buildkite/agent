package clicommand

import (
	"fmt"
	"os"

	"github.com/buildkite/agent/agent"
	"github.com/buildkite/agent/cliconfig"
	"github.com/buildkite/agent/logger"
	"github.com/buildkite/agent/run"
	"github.com/buildkite/shellwords"
	"github.com/urfave/cli"
)

var RunHelpDescription = `Usage:

   buildkite-agent run <file> [arguments...]

Description:

   Run steps from a pipeline.yml file locally.

   This allows for testing of plugins and interaction between your steps prior to
   uploading to buildkite.com.

Example:

   $ buildkite-agent run .buildkite/pipeline.yml
   $ buildkite-agent run .buildkite/pipeline.yml --step "Run Tests"`

type RunConfig struct {
	FilePath        string `cli:"arg:0" label:"pipeline.yml paths"`
	Step            string `cli:"step"`
	BootstrapScript string `cli:"bootstrap-script" normalize:"commandpath"`
	BuildPath       string `cli:"build-path" normalize:"filepath" validate:"required"`
	HooksPath       string `cli:"hooks-path" normalize:"filepath"`
	PluginsPath     string `cli:"plugins-path" normalize:"filepath"`
	NoColor         bool   `cli:"no-color"`
	NoInterpolation bool   `cli:"no-interpolation"`
	Debug           bool   `cli:"debug"`
	DebugHTTP       bool   `cli:"debug-http"`
}

var RunCommand = cli.Command{
	Name:        "run",
	Usage:       "Run steps from a pipeline.yml file locally.",
	Description: RunHelpDescription,
	Flags: []cli.Flag{
		cli.BoolFlag{
			Name:   "no-interpolation",
			Usage:  "Skip variable interpolation on the pipeline",
			EnvVar: "BUILDKITE_PIPELINE_NO_INTERPOLATION",
		},
		cli.StringFlag{
			Name:  "step",
			Value: "",
			Usage: "Run a particular step by name, otherwise all are run",
		},
		cli.StringFlag{
			Name:   "build-path",
			Value:  "",
			Usage:  "Path to where the builds will run from",
			EnvVar: "BUILDKITE_BUILD_PATH",
		},
		cli.StringFlag{
			Name:   "hooks-path",
			Value:  "",
			Usage:  "Directory where the hook scripts are found",
			EnvVar: "BUILDKITE_HOOKS_PATH",
		},
		cli.StringFlag{
			Name:   "plugins-path",
			Value:  "",
			Usage:  "Directory where the plugins are saved to",
			EnvVar: "BUILDKITE_PLUGINS_PATH",
		},
		NoColorFlag,
		DebugFlag,
		DebugHTTPFlag,
	},
	Action: func(c *cli.Context) {
		// The configuration will be loaded into this struct
		cfg := RunConfig{}

		// Setup the config loader. You'll see that we also path paths to
		// potential config files. The loader will use the first one it finds.
		loader := cliconfig.Loader{
			CLI:                    c,
			Config:                 &cfg,
			DefaultConfigFilePaths: DefaultConfigFilePaths(),
		}

		// Load the configuration
		if err := loader.Load(); err != nil {
			logger.Fatal("%s", err)
		}

		// Setup the any global configuration options
		HandleGlobalFlags(cfg)

		// Set a useful default for the bootstrap script
		if cfg.BootstrapScript == "" {
			cfg.BootstrapScript = fmt.Sprintf("%s bootstrap", shellwords.Quote(os.Args[0]))
		}

		// Read Pipeline from either FilePath, Stdin or Search
		filename, input, err := HandlePipelineFileFlag(cfg.FilePath)
		if err != nil {
			logger.Fatal("%v", err)
		}

		var parsed interface{}

		// Parse the pipeline
		parsed, err = agent.PipelineParser{
			Filename:        filename,
			Pipeline:        input,
			NoInterpolation: cfg.NoInterpolation,
		}.Parse()
		if err != nil {
			logger.Fatal("Pipeline parsing of \"%s\" failed (%s)", filename, err)
		}

		runner := run.Runner{
			Pipeline:        parsed,
			Step:            cfg.Step,
			BuildPath:       cfg.BuildPath,
			PluginPath:      cfg.PluginsPath,
			BootstrapScript: cfg.BootstrapScript,
		}

		if err := runner.Run(); err != nil {
			logger.Fatal("%v", err)
		}
	},
}
