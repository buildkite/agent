package clicommand

import (
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/buildkite/agent/agent"
	"github.com/buildkite/agent/api"
	"github.com/buildkite/agent/cliconfig"
	"github.com/buildkite/agent/logger"
	"github.com/buildkite/agent/retry"
	"github.com/buildkite/agent/stdin"
	"github.com/codegangsta/cli"
)

var PipelineUploadHelpDescription = `Usage:

   buildkite-agent pipeline upload <file> [arguments...]

Description:

   Allows you to change the pipeline of a running build by uploading either a
   YAML (recommended) or JSON configuration file. If no configuration file is
   provided, the command looks for the file in the following locations:

   - buildkite.yml
   - buildkite.yaml
   - buildkite.json
   - .buildkite/pipeline.yml
   - .buildkite/pipeline.yaml
   - .buildkite/pipeline.json

   You can also pipe build pipelines to the command allowing you to create
   scripts that generate dynamic pipelines.

Example:

   $ buildkite-agent pipeline upload
   $ buildkite-agent pipeline upload my-custom-pipeline.yml
   $ ./script/dynamic_step_generator | buildkite-agent pipeline upload --format json`

type PipelineUploadConfig struct {
	FilePath         string `cli:"arg:0" label:"upload paths"`
	Format           string `cli:"format"`
	Replace          bool   `cli:"replace"`
	Job              string `cli:"job" validate:"required"`
	AgentAccessToken string `cli:"agent-access-token" validate:"required"`
	Endpoint         string `cli:"endpoint" validate:"required"`
	NoColor          bool   `cli:"no-color"`
	Debug            bool   `cli:"debug"`
	DebugHTTP        bool   `cli:"debug-http"`
}

var PipelineUploadCommand = cli.Command{
	Name:        "upload",
	Usage:       "Uploads a description of a build pipeline adds it to the currently running build after the current job.",
	Description: PipelineUploadHelpDescription,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:   "format",
			Value:  "",
			Usage:  "The file format of the pipeline (yaml, json). This argument is required when reading a build pipeline via STDIN.",
			EnvVar: "BUILDKITE_PIPELINE_FORMAT",
		},
		cli.BoolFlag{
			Name:   "replace",
			Usage:  "Replace the rest of the existing pipeline with the steps uploaded. Jobs that are already running are not removed.",
			EnvVar: "BUILDKITE_PIPELINE_REPLACE",
		},
		cli.StringFlag{
			Name:   "job",
			Value:  "",
			Usage:  "The job that is making the changes to it's build",
			EnvVar: "BUILDKITE_JOB_ID",
		},
		AgentAccessTokenFlag,
		EndpointFlag,
		NoColorFlag,
		DebugFlag,
		DebugHTTPFlag,
	},
	Action: func(c *cli.Context) {
		// The configuration will be loaded into this struct
		cfg := PipelineUploadConfig{}

		// Load the configuration
		loader := cliconfig.Loader{CLI: c, Config: &cfg}
		if err := loader.Load(); err != nil {
			logger.Fatal("%s", err)
		}

		// Setup the any global configuration options
		HandleGlobalFlags(cfg)

		// Find the pipeline file either from STDIN or the first
		// argument
		var input []byte
		var err error
		var filename string
		var format string

		if cfg.FilePath != "" {
			logger.Info("Reading pipeline config from \"%s\"", cfg.FilePath)

			filename = filepath.Base(cfg.FilePath)
			input, err = ioutil.ReadFile(cfg.FilePath)
			if err != nil {
				logger.Fatal("Failed to read file: %s", err)
			}
		} else if stdin.IsPipe() {
			logger.Info("Reading pipeline config from STDIN")

			// Make sure a format was passed
			if cfg.Format == "" {
				logger.Fatal("A format defined with `--format (yaml or json)` is required when reading a config via STDIN, for example (./script/dynamic_step_generator | buildkite-agent pipeline upload --format json). See `buildkite-agent pipeline upload --help` for more information.")
			} else if cfg.Format != "yaml" || cfg.Format != "json" {
				logger.Fatal("Unknown pipeline format `%s` - only `yaml` and `json` are supported. See `buildkite-agent pipeline upload --help` for more information.", cfg.Format)
			}

			format = cfg.Format

			// Now we can read from STDIN
			input, err = ioutil.ReadAll(os.Stdin)
			if err != nil {
				logger.Fatal("Failed to read from STDIN: %s", err)
			}
		} else {
			logger.Info("Searching for pipeline config...")

			paths := []string{
				"buildkite.yml",
				"buildkite.yaml",
				"buildkite.json",
				filepath.FromSlash(".buildkite/pipeline.yml"),
				filepath.FromSlash(".buildkite/pipeline.yaml"),
				filepath.FromSlash(".buildkite/pipeline.json"),
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
				logger.Fatal("Found multiple configuration files: %s. Please only have 1 configuration file present.", strings.Join(exists, ", "))
			} else if len(exists) == 0 {
				logger.Fatal("Could not find a default pipeline configuration file. See `buildkite-agent pipeline upload --help` for more information.")
			}

			found := exists[0]

			logger.Info("Found config file \"%s\"", found)

			// Read the default file
			filename = path.Base(found)
			input, err = ioutil.ReadFile(found)
			if err != nil {
				logger.Fatal("Failed to read file \"%s\" (%s)", found, err)
			}
		}

		if len(input) == 0 {
			logger.Fatal("Config file is empty")
		}

		var parsed []byte

		logger.Debug("Parsing pipeline...")

		// Interpolate the environment variables in the pipeline
		parsed, err = agent.EnvironmentVariableInterpolator{Data: input}.Interpolate()
		if err != nil {
			logger.Fatal("Pipeline parsing of \"%s\" failed (%s)", filename, err)
		}

		// Create the API client
		client := agent.APIClient{
			Endpoint: cfg.Endpoint,
			Token:    cfg.AgentAccessToken,
		}.Create()

		// Generate a UUID that will identifiy this pipeline change. We
		// do this outside of the retry loop because we want this UUID
		// to be the same for each attempt at updating the pipeline.
		uuid := api.NewUUID()

		// Retry the pipeline upload a few times before giving up
		err = retry.Do(func(s *retry.Stats) error {
			_, err = client.Pipelines.Upload(cfg.Job, &api.Pipeline{UUID: uuid, Data: parsed, FileName: filename, Replace: cfg.Replace})
			if err != nil {
				logger.Warn("%s (%s)", err, s)
			}

			return err
		}, &retry.Config{Maximum: 5, Interval: 1 * time.Second})
		if err != nil {
			logger.Fatal("Failed to upload and process pipeline: %s", err)
		}

		logger.Info("Successfully uploaded and parsed pipeline config")
	},
}
