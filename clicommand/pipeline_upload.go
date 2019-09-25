package clicommand

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/buildkite/agent/agent"
	"github.com/buildkite/agent/api"
	"github.com/buildkite/agent/cliconfig"
	"github.com/buildkite/agent/env"
	"github.com/buildkite/agent/retry"
	"github.com/buildkite/agent/stdin"
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
   - buildkite/pipeline.yml
   - buildkite/pipeline.yaml
   - buildkite/pipeline.json

   You can also pipe build pipelines to the command allowing you to create
   scripts that generate dynamic pipelines.

Example:

   $ buildkite-agent pipeline upload
   $ buildkite-agent pipeline upload my-custom-pipeline.yml
   $ ./script/dynamic_step_generator | buildkite-agent pipeline upload`

type PipelineUploadConfig struct {
	FilePath        string `cli:"arg:0" label:"upload paths"`
	Replace         bool   `cli:"replace"`
	Job             string `cli:"job"`
	DryRun          bool   `cli:"dry-run"`
	NoInterpolation bool   `cli:"no-interpolation"`

	// Global flags
	Debug   bool   `cli:"debug"`
	NoColor bool   `cli:"no-color"`
	Profile string `cli:"profile"`

	// API config
	DebugHTTP        bool   `cli:"debug-http"`
	AgentAccessToken string `cli:"agent-access-token"`
	Endpoint         string `cli:"endpoint" validate:"required"`
	NoHTTP2          bool   `cli:"no-http2"`
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
			Name:   "dry-run",
			Usage:  "Rather than uploading the pipeline, it will be echoed to stdout",
			EnvVar: "BUILDKITE_PIPELINE_UPLOAD_DRY_RUN",
		},
		cli.BoolFlag{
			Name:   "no-interpolation",
			Usage:  "Skip variable interpolation the pipeline when uploaded",
			EnvVar: "BUILDKITE_PIPELINE_NO_INTERPOLATION",
		},

		// API Flags
		AgentAccessTokenFlag,
		EndpointFlag,
		NoHTTP2Flag,
		DebugHTTPFlag,

		// Global flags
		NoColorFlag,
		DebugFlag,
		ProfileFlag,
	},
	Action: func(c *cli.Context) {
		// The configuration will be loaded into this struct
		cfg := PipelineUploadConfig{}

		l := CreateLogger(&cfg)

		// Load the configuration
		if err := cliconfig.Load(c, l, &cfg); err != nil {
			l.Fatal("%s", err)
		}

		// Setup any global configuration options
		done := HandleGlobalFlags(l, cfg)
		defer done()

		// Find the pipeline file either from STDIN or the first
		// argument
		var input []byte
		var err error
		var filename string

		if cfg.FilePath != "" {
			l.Info("Reading pipeline config from \"%s\"", cfg.FilePath)

			filename = filepath.Base(cfg.FilePath)
			input, err = ioutil.ReadFile(cfg.FilePath)
			if err != nil {
				l.Fatal("Failed to read file: %s", err)
			}
		} else if stdin.IsReadable() {
			l.Info("Reading pipeline config from STDIN")

			// Actually read the file from STDIN
			input, err = ioutil.ReadAll(os.Stdin)
			if err != nil {
				l.Fatal("Failed to read from STDIN: %s", err)
			}
		} else {
			l.Info("Searching for pipeline config...")

			paths := []string{
				"buildkite.yml",
				"buildkite.yaml",
				"buildkite.json",
				filepath.FromSlash(".buildkite/pipeline.yml"),
				filepath.FromSlash(".buildkite/pipeline.yaml"),
				filepath.FromSlash(".buildkite/pipeline.json"),
				filepath.FromSlash("buildkite/pipeline.yml"),
				filepath.FromSlash("buildkite/pipeline.yaml"),
				filepath.FromSlash("buildkite/pipeline.json"),
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
			input, err = ioutil.ReadFile(found)
			if err != nil {
				l.Fatal("Failed to read file \"%s\" (%s)", found, err)
			}
		}

		// Make sure the file actually has something in it
		if len(input) == 0 {
			l.Fatal("Config file is empty")
		}

		// Load environment to pass into parser
		environ := env.FromSlice(os.Environ())

		// resolve BUILDKITE_COMMIT based on the local git repo
		if commitRef, ok := environ.Get(`BUILDKITE_COMMIT`); ok {
			cmdOut, err := exec.Command(`git`, `rev-parse`, commitRef).Output()
			if err != nil {
				l.Warn("Error running git rev-parse %q: %v", commitRef, err)
			} else {
				trimmedCmdOut := strings.TrimSpace(string(cmdOut))
				l.Info("Updating BUILDKITE_COMMIT to %q", trimmedCmdOut)
				environ.Set(`BUILDKITE_COMMIT`, trimmedCmdOut)
			}
		}

		// Parse the pipeline
		result, err := agent.PipelineParser{
			Env:             environ,
			Filename:        filename,
			Pipeline:        input,
			NoInterpolation: cfg.NoInterpolation,
		}.Parse()
		if err != nil {
			l.Fatal("Pipeline parsing of \"%s\" failed (%s)", filename, err)
		}

		// In dry-run mode we just output the generated pipeline to stdout
		if cfg.DryRun {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")

			// Dump json indented to stdout. All logging happens to stderr
			// this can be used with other tools to get interpolated json
			if err := enc.Encode(result); err != nil {
				l.Fatal("%#v", err)
			}

			return
		}

		// Check we have a job id set if not in dry run
		if cfg.Job == "" {
			l.Fatal("Missing job parameter. Usually this is set in the environment for a Buildkite job via BUILDKITE_JOB_ID.")
		}

		// Check we have an agent access token if not in dry run
		if cfg.AgentAccessToken == "" {
			l.Fatal("Missing agent-access-token parameter. Usually this is set in the environment for a Buildkite job via BUILDKITE_AGENT_ACCESS_TOKEN.")
		}

		// Create the API client
		client := api.NewClient(l, loadAPIClientConfig(cfg, `AgentAccessToken`))

		// Generate a UUID that will identifiy this pipeline change. We
		// do this outside of the retry loop because we want this UUID
		// to be the same for each attempt at updating the pipeline.
		uuid := api.NewUUID()

		// Retry the pipeline upload a few times before giving up
		err = retry.Do(func(s *retry.Stats) error {
			_, err = client.UploadPipeline(cfg.Job, &api.Pipeline{UUID: uuid, Pipeline: result, Replace: cfg.Replace})
			if err != nil {
				l.Warn("%s (%s)", err, s)

				// 422 responses will always fail no need to retry
				if apierr, ok := err.(*api.ErrorResponse); ok && apierr.Response.StatusCode == 422 {
					l.Error("Unrecoverable error, skipping retries")
					s.Break()
				}
			}

			return err
			// On a server error, it means there is downtime or other problems, we
			// need to retry. Let's retry every 5 seconds, for a total of 5 minutes.
		}, &retry.Config{Maximum: 60, Interval: 5 * time.Second})
		if err != nil {
			l.Fatal("Failed to upload and process pipeline: %s", err)
		}

		l.Info("Successfully uploaded and parsed pipeline config")
	},
}
