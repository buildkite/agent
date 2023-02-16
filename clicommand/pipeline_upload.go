package clicommand

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/buildkite/agent/v3/agent"
	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/cliconfig"
	"github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/job/shell"
	"github.com/buildkite/agent/v3/redaction"
	"github.com/buildkite/agent/v3/stdin"
	"github.com/urfave/cli"
)

const pipelineUploadHelpDescription = `Usage:

   buildkite-agent pipeline upload [file] [options...]

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
   scripts that generate dynamic pipelines. The configuration file has a
   limit of 500 steps per file. Configuration files with over 500 steps
   must be split into multiple files and uploaded in separate steps.

Example:

   $ buildkite-agent pipeline upload
   $ buildkite-agent pipeline upload my-custom-pipeline.yml
   $ ./script/dynamic_step_generator | buildkite-agent pipeline upload`

type PipelineUploadConfig struct {
	FilePath        string   `cli:"arg:0" label:"upload paths"`
	Replace         bool     `cli:"replace"`
	Job             string   `cli:"job"`
	DryRun          bool     `cli:"dry-run"`
	NoInterpolation bool     `cli:"no-interpolation"`
	RedactedVars    []string `cli:"redacted-vars" normalize:"list"`
	RejectSecrets   bool     `cli:"reject-secrets"`

	// Global flags
	Debug       bool     `cli:"debug"`
	LogLevel    string   `cli:"log-level"`
	NoColor     bool     `cli:"no-color"`
	Experiments []string `cli:"experiment" normalize:"list"`
	Profile     string   `cli:"profile"`

	// API config
	DebugHTTP        bool   `cli:"debug-http"`
	AgentAccessToken string `cli:"agent-access-token" validate:"required"`
	Endpoint         string `cli:"endpoint" validate:"required"`
	NoHTTP2          bool   `cli:"no-http2"`
}

var PipelineUploadCommand = cli.Command{
	Name:        "upload",
	Usage:       "Uploads a description of a build pipeline adds it to the currently running build after the current job",
	Description: pipelineUploadHelpDescription,
	Flags: []cli.Flag{
		cli.BoolFlag{
			Name:   "replace",
			Usage:  "Replace the rest of the existing pipeline with the steps uploaded. Jobs that are already running are not removed.",
			EnvVar: "BUILDKITE_PIPELINE_REPLACE",
		},
		cli.StringFlag{
			Name:   "job",
			Value:  "",
			Usage:  "The job that is making the changes to its build",
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
		cli.BoolFlag{
			Name:   "reject-secrets",
			Usage:  "When true, fail the pipeline upload early if the pipeline contains secrets",
			EnvVar: "BUILDKITE_AGENT_PIPELINE_UPLOAD_REJECT_SECRETS",
		},

		// API Flags
		AgentAccessTokenFlag,
		EndpointFlag,
		NoHTTP2Flag,
		DebugHTTPFlag,

		// Global flags
		NoColorFlag,
		DebugFlag,
		LogLevelFlag,
		ExperimentsFlag,
		ProfileFlag,
		RedactedVars,
	},
	Action: func(c *cli.Context) {
		ctx := context.Background()

		// The configuration will be loaded into this struct
		cfg := PipelineUploadConfig{}

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
			input, err = os.ReadFile(found)
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
		if commitRef, ok := environ.Get("BUILDKITE_COMMIT"); ok {
			cmdOut, err := exec.Command("git", "rev-parse", commitRef).Output()
			if err != nil {
				l.Warn("Error running git rev-parse %q: %v", commitRef, err)
			} else {
				trimmedCmdOut := strings.TrimSpace(string(cmdOut))
				l.Info("Updating BUILDKITE_COMMIT to %q", trimmedCmdOut)
				environ.Set("BUILDKITE_COMMIT", trimmedCmdOut)
			}
		}

		src := filename
		if src == "" {
			src = "(stdin)"
		}

		// Parse the pipeline
		parser := agent.PipelineParser{
			Env:             environ,
			Filename:        filename,
			Pipeline:        input,
			NoInterpolation: cfg.NoInterpolation,
		}
		result, err := parser.Parse()
		if err != nil {
			l.Fatal("Pipeline parsing of \"%s\" failed (%s)", src, err)
		}

		if len(cfg.RedactedVars) > 0 {
			needles := redaction.GetKeyValuesToRedact(shell.StderrLogger, cfg.RedactedVars, env.FromSlice(os.Environ()).Dump())

			serialisedPipeline, err := result.MarshalJSON()
			if err != nil {
				l.Fatal("Couldn’t scan the %q pipeline for redacted variables. This parsed pipeline could not be serialized, ensure the pipeline YAML is valid, or ignore interpolated secrets for this upload by passing --redacted-vars=''. (%s)", src, err)
			}

			stringifiedserialisedPipeline := string(serialisedPipeline)

			secretsFound := make([]string, 0, len(needles))
			for needleKey, needle := range needles {
				if strings.Contains(stringifiedserialisedPipeline, needle) {
					secretsFound = append(secretsFound, needleKey)
				}
			}

			if len(secretsFound) > 0 {
				if cfg.RejectSecrets {
					l.Fatal("Pipeline %q contains values interpolated from the following secret environment variables: %v, and cannot be uploaded to Buildkite", src, secretsFound)
				} else {
					l.Warn("Pipeline %q contains values interpolated from the following secret environment variables: %v, which could leak sensitive information into the Buildkite UI.", src, secretsFound)
					l.Warn("This pipeline will still be uploaded, but if you'd like to to prevent this from happening, you can use the `--reject-secrets` cli flag, or the `BUILDKITE_AGENT_PIPELINE_UPLOAD_REJECT_SECRETS` environment variable, which will make the `buildkite-agent pipeline upload` command fail if it finds secrets in the pipeline.")
					l.Warn("The behaviour in the above flags will become default in Buildkite Agent v4")
				}
			}
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

		uploader := &agent.PipelineUploader{
			Client: api.NewClient(l, loadAPIClientConfig(cfg, "AgentAccessToken")),
			JobID:  cfg.Job,
			Change: &api.PipelineChange{
				UUID:     api.NewUUID(),
				Replace:  cfg.Replace,
				Pipeline: result,
			},
			RetrySleepFunc: time.Sleep,
		}
		if err := uploader.Upload(ctx, l); err != nil {
			l.Fatal("%v", err)
		}

		l.Info("Successfully uploaded and parsed pipeline config")
	},
}
