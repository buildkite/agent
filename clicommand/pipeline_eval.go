package clicommand

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/buildkite/agent/v3/cliconfig"
	"github.com/buildkite/agent/v3/resources"
	"github.com/buildkite/agent/v3/stdin"
	"github.com/buildkite/yaml"
	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/console"
	"github.com/dop251/goja_nodejs/process"
	"github.com/dop251/goja_nodejs/require"
	"github.com/urfave/cli"
)

const evalDescription = `Usage:
  buildkite-agent pipeline eval [options]

Description:
   Something something JavaScript?

Example:
   $ buildkite-agent pipeline eval buildkite.js

   Evaluates buildkite.js as JavaScript and perhaps uploads the stdout as JSON/YAML pipeline?
`

type PipelineEvalConfig struct {
	FilePath string `cli:"arg:0" label:"upload paths"`

	// Global flags
	Debug       bool     `cli:"debug"`
	LogLevel    string   `cli:"log-level"`
	NoColor     bool     `cli:"no-color"`
	Experiments []string `cli:"experiment" normalize:"list"`
	Profile     string   `cli:"profile"`
}

var PipelineEvalCommand = cli.Command{
	Name:        "eval",
	Usage:       "Evaluates a JavaScript pipeline",
	Description: evalDescription,
	Flags: []cli.Flag{
		// Global flags
		NoColorFlag,
		DebugFlag,
		LogLevelFlag,
		ExperimentsFlag,
		ProfileFlag,
	},
	Action: func(c *cli.Context) error {
		// The configuration will be loaded into this struct
		cfg := PipelineEvalConfig{}

		loader := cliconfig.Loader{CLI: c, Config: &cfg}
		warnings, err := loader.Load()
		if err != nil {
			fmt.Printf("%s", err)
			os.Exit(1)
		}

		l := CreateLogger(&cfg)

		// Now that we have a logger, log out the warnings that loading config generated
		for _, warning := range warnings {
			l.Warn("%s", warning)
		}

		// Setup any global configuration options
		done := HandleGlobalFlags(l, cfg)
		defer done()

		// Find the pipeline file either from STDIN or the first
		// argument
		var input []byte
		var filename string

		if cfg.FilePath != "" {
			l.Info("Reading pipeline config from \"%s\"", cfg.FilePath)

			filename = filepath.Base(cfg.FilePath)
			input, err = os.ReadFile(cfg.FilePath)
			if err != nil {
				l.Fatal("Failed to read file: %s", err)
			}
		} else if stdin.IsReadable() {
			l.Info("Reading pipeline config from STDIN")

			// Actually read the file from STDIN
			input, err = io.ReadAll(os.Stdin)
			if err != nil {
				l.Fatal("Failed to read from STDIN: %s", err)
			}
		} else {
			l.Info("Searching for pipeline config...")

			paths := []string{
				"buildkite.js",
				filepath.FromSlash(".buildkite/buildkite.js"),
				filepath.FromSlash("buildkite/buildkite.js"),
			}

			// Collect all the files that exist
			exists := []string{}
			for _, path := range paths {
				if _, err := os.Stat(path); err == nil {
					exists = append(exists, path)
				}
			}

			// If more than 1 of the config files exist, throw an
			// error. There can only be one!!
			if len(exists) > 1 {
				l.Fatal("Found multiple configuration files: %s. Please only have 1 configuration file present.", strings.Join(exists, ", "))
			} else if len(exists) == 0 {
				l.Fatal("Could not find a default pipeline configuration file. See `buildkite-agent pipeline upload --help` for more information.")
			}

			found := exists[0]

			l.Info("Found config file \"%s\"", found)

			// Read the default file
			filename = path.Base(found)
			input, err = os.ReadFile(found)
			if err != nil {
				l.Fatal("Failed to read file \"%s\" (%s)", found, err)
			}
		}

		if err := evalJS(filename, input, c.App.Writer); err != nil {
			panic(err)
		}
		return nil
	},
}

func evalJS(filename string, input []byte, output io.Writer) error {
	runtime := goja.New()
	runtime.SetFieldNameMapper(goja.TagFieldNameMapper("json", true))

	// Add support for require() CommonJS modules.
	// require("buildkite/*") is handled by embedded resources/node_modules/buildkite/* filesystem.
	// Other paths are loaded from the host filesystem.
	registry := require.NewRegistry(
		require.WithLoader(func(name string) ([]byte, error) {
			if !strings.HasPrefix(name, "node_modules/buildkite/") {
				return require.DefaultSourceLoader(name)
			}
			res := resources.FS
			data, err := res.ReadFile(name)
			if errors.Is(err, fs.ErrNotExist) {
				return nil, require.ModuleFileDoesNotExistError
			} else if err != nil {
				return nil, err
			}
			return data, nil
		}),
	)
	registry.Enable(runtime)

	// Add basic utilities
	console.Enable(runtime) // console.log()
	process.Enable(runtime) // process.env()

	// provide plugin() as a native module (implemented in Go)
	registry.RegisterNativeModule("buildkite/plugin", func(runtime *goja.Runtime, module *goja.Object) {
		module.Set("exports", func(call goja.FunctionCall) goja.Value {
			name := call.Argument(0)
			ref := call.Argument(1)
			config := call.Argument(2)
			plugin := runtime.NewObject()
			plugin.Set(name.String()+"#"+ref.String(), config)
			return plugin
		})
	})

	// provide assignable module.exports for Pipeline result
	rootModule := runtime.NewObject()
	rootModule.Set("exports", runtime.NewObject())
	err := runtime.Set("module", rootModule)
	if err != nil {
		return err
	}

	if filename == "" {
		filename = "(stdin)"
	}

	v, err := runtime.RunScript(filename, string(input))
	if err != nil {
		panic(err)
	}

	y, err := yaml.Marshal(v.Export())
	if err != nil {
		return err
	}

	n, err := output.Write(y)
	if err != nil {
		return nil
	}
	if n != len(y) {
		return errors.New("short write")
	}

	return nil
}
