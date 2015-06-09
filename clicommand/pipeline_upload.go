package clicommand

import (
	"github.com/andrew-d/go-termutil"
	"github.com/buildkite/agent/agent"
	"github.com/buildkite/agent/api"
	"github.com/buildkite/agent/cliconfig"
	"github.com/buildkite/agent/logger"
	"github.com/buildkite/agent/retry"
	"github.com/codegangsta/cli"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"
)

var PipelineUploadHelpDescription = `Usage:

   buildkite-agent pipeline upload <file> [arguments...]

Description:

   Allows you to change the pipeline of a running build by uploading a
   JSON configuration file. By default, we try to upload
   .buildkite/steps.json, but you can customize which file to use.

Example:

   $ buildkite-agent pipeline upload
   $ buildkite-agent pipeline upload my-custom-steps.json
   $ ./script/dynamic_step_generator | buildkite-agent pipeline upload`

type PipelineUploadConfig struct {
	FilePath         string `cli:"arg:0" label:"upload paths"`
	Job              string `cli:"job" validate:"required"`
	AgentAccessToken string `cli:"agent-access-token" validate:"required"`
	Endpoint         string `cli:"endpoint" validate:"required"`
	NoColor          bool   `cli:"no-color"`
	Debug            bool   `cli:"debug"`
}

var PipelineUploadCommand = cli.Command{
	Name:        "upload",
	Usage:       "Upload a description of the build pipleine and change the running build",
	Description: PipelineUploadHelpDescription,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:   "job",
			Value:  "",
			Usage:  "The job that is making the changes to it's build",
			EnvVar: "BUILDKITE_JOB_ID",
		},
		AgentAccessTokenFlag,
		EndpointFlag,
		DebugFlag,
		NoColorFlag,
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

		if cfg.FilePath != "" {
			filename = filepath.Base(cfg.FilePath)
			input, err = ioutil.ReadFile(cfg.FilePath)
			if err != nil {
				logger.Fatal("Failed to read file: %s", err)
			}
		} else if !termutil.Isatty(os.Stdin.Fd()) {
			input, err = ioutil.ReadAll(os.Stdin)
			if err != nil {
				logger.Fatal("Failed to read from STDIN: %s", err)
			}
		} else {
			// Default to opening .buildkite/steps.json
			filename = "steps.json"
			path, _ := filepath.Abs(".buildkite/" + filename)
			input, err = ioutil.ReadFile(path)
			if err != nil {
				logger.Fatal("Failed to read file: %s", err)
			}
		}

		// Create the API client
		client := agent.APIClient{
			Endpoint: cfg.Endpoint,
			Token:    cfg.AgentAccessToken,
		}.Create()

		// Retry the pipeline upload a few times before giving up
		err = retry.Do(func(s *retry.Stats) error {
			_, err = client.Pipelines.Upload(cfg.Job, &api.Pipeline{Data: input, FileName: filename})
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
