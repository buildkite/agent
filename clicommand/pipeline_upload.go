package clicommand

import (
	"time"

	"github.com/buildkite/agent/agent"
	"github.com/buildkite/agent/api"
	"github.com/buildkite/agent/cliconfig"
	"github.com/buildkite/agent/logger"
	"github.com/buildkite/agent/retry"
	"github.com/urfave/cli"
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
   $ ./script/dynamic_step_generator | buildkite-agent pipeline upload`

type PipelineUploadConfig struct {
	FilePath         string `cli:"arg:0" label:"upload paths"`
	Replace          bool   `cli:"replace"`
	Job              string `cli:"job" validate:"required"`
	AgentAccessToken string `cli:"agent-access-token" validate:"required"`
	Endpoint         string `cli:"endpoint" validate:"required"`
	NoColor          bool   `cli:"no-color"`
	NoInterpolation  bool   `cli:"no-interpolation"`
	Debug            bool   `cli:"debug"`
	DebugHTTP        bool   `cli:"debug-http"`
}

var PipelineUploadCommand = cli.Command{
	Name:        "upload",
	Usage:       "Uploads a description of a build pipeline adds it to the currently running build after the current job.",
	Description: PipelineUploadHelpDescription,
	Flags: []cli.Flag{
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
		cli.BoolFlag{
			Name:   "no-interpolation",
			Usage:  "Skip variable interpolation the pipeline when uploaded",
			EnvVar: "BUILDKITE_PIPELINE_NO_INTERPOLATION",
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
			_, err = client.Pipelines.Upload(cfg.Job, &api.Pipeline{UUID: uuid, Pipeline: parsed, Replace: cfg.Replace})
			if err != nil {
				logger.Warn("%s (%s)", err, s)
				apierr := err.(*api.ErrorResponse)
				// 422 responses will always fail no need to retry
				if apierr.Response.StatusCode == 422 {
					logger.Error("Unrecoverable error, skipping retries")
					s.Break()
				}
			}

			return err
		}, &retry.Config{Maximum: 5, Interval: 1 * time.Second})
		if err != nil {
			logger.Fatal("Failed to upload and process pipeline: %s", err)
		}

		logger.Info("Successfully uploaded and parsed pipeline config")
	},
}
