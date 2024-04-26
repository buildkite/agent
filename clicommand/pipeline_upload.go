package clicommand

import (
	"context"
	"encoding/json"
	"errors"
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
	"github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/internal/job/shell"
	"github.com/buildkite/agent/v3/internal/redact"
	"github.com/buildkite/agent/v3/internal/replacer"
	"github.com/buildkite/agent/v3/internal/stdin"
	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/agent/v3/tracetools"
	"github.com/buildkite/go-pipeline"
	"github.com/buildkite/go-pipeline/jwkutil"
	"github.com/buildkite/go-pipeline/ordered"
	"github.com/buildkite/go-pipeline/signature"
	"github.com/buildkite/go-pipeline/warning"
	"github.com/urfave/cli"
	"gopkg.in/yaml.v3"
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
	Job             string   `cli:"job"` // required, but not in dry-run mode
	DryRun          bool     `cli:"dry-run"`
	DryRunFormat    string   `cli:"format"`
	NoInterpolation bool     `cli:"no-interpolation"`
	RedactedVars    []string `cli:"redacted-vars" normalize:"list"`
	RejectSecrets   bool     `cli:"reject-secrets"`

	// Used for signing
	JWKSFile  string `cli:"jwks-file"`
	JWKSKeyID string `cli:"jwks-key-id"`

	// Global flags
	Debug       bool     `cli:"debug"`
	LogLevel    string   `cli:"log-level"`
	NoColor     bool     `cli:"no-color"`
	Experiments []string `cli:"experiment" normalize:"list"`
	Profile     string   `cli:"profile"`

	// API config
	DebugHTTP        bool   `cli:"debug-http"`
	AgentAccessToken string `cli:"agent-access-token"` // required, but not in dry-run mode
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
		cli.StringFlag{
			Name:   "format",
			Usage:  "In dry-run mode, specifies the form to output the pipeline in. Must be one of: json,yaml",
			Value:  "json",
			EnvVar: "BUILDKITE_PIPELINE_UPLOAD_DRY_RUN_FORMAT",
		},
		cli.BoolFlag{
			Name:   "no-interpolation",
			Usage:  "Skip variable interpolation into the pipeline prior to upload",
			EnvVar: "BUILDKITE_PIPELINE_NO_INTERPOLATION",
		},
		cli.BoolFlag{
			Name:   "reject-secrets",
			Usage:  "When true, fail the pipeline upload early if the pipeline contains secrets",
			EnvVar: "BUILDKITE_AGENT_PIPELINE_UPLOAD_REJECT_SECRETS",
		},

		// Note: changes to these environment variables need to be reflected in the environment created
		// in the job runner. At the momenet, that's at agent/job_runner.go:500-507
		cli.StringFlag{
			Name:   "jwks-file",
			Usage:  "Path to a file containing a JWKS. Passing this flag enables pipeline signing",
			EnvVar: "BUILDKITE_AGENT_JWKS_FILE",
		},
		cli.StringFlag{
			Name:   "jwks-key-id",
			Usage:  "The JWKS key ID to use when signing the pipeline. Required when using a JWKS",
			EnvVar: "BUILDKITE_AGENT_JWKS_KEY_ID",
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
	Action: func(c *cli.Context) error {
		ctx := context.Background()
		ctx, cfg, l, _, done := setupLoggerAndConfig[PipelineUploadConfig](ctx, c)
		defer done()

		// Find the pipeline either from STDIN or the first argument
		var input *os.File
		var filename string

		switch {
		case cfg.FilePath != "":
			l.Info("Reading pipeline config from %q", cfg.FilePath)

			filename = filepath.Base(cfg.FilePath)
			file, err := os.Open(cfg.FilePath)
			if err != nil {
				return fmt.Errorf("failed to read file: %w", err)
			}
			defer file.Close()
			input = file

		case stdin.IsReadable():
			l.Info("Reading pipeline config from STDIN")

			// Actually read the file from STDIN
			input = os.Stdin

		default:
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
				return fmt.Errorf("found multiple configuration files: %s. Please only have 1 configuration file present.", strings.Join(exists, ", "))
			}
			if len(exists) == 0 {
				return fmt.Errorf("could not find a default pipeline configuration file. See `buildkite-agent pipeline upload --help` for more information.")
			}

			found := exists[0]

			l.Info("Found config file %q", found)

			// Read the default file
			filename = path.Base(found)
			file, err := os.Open(found)
			if err != nil {
				return fmt.Errorf("failed to read file %q: %w", found, err)
			}
			defer file.Close()
			input = file
		}

		// Make sure the file actually has something in it
		if input != os.Stdin {
			fi, err := input.Stat()
			if err != nil {
				return fmt.Errorf("couldn't stat pipeline configuration file %q: %v", input.Name(), err)
			}
			if fi.Size() == 0 {
				return fmt.Errorf("pipeline file %q is empty", input.Name())
			}
		}

		environ := env.FromSlice(os.Environ())

		if !cfg.NoInterpolation {
			// Load environment to pass into parser

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
		}

		src := filename
		if src == "" {
			src = "(stdin)"
		}

		result, err := cfg.parseAndInterpolate(src, input, environ)
		if w := warning.As(err); w != nil {
			l.Warn("There were some issues with the pipeline input - pipeline upload will proceed, but might not succeed:\n%v", w)
		} else if err != nil {
			return err
		}

		if len(cfg.RedactedVars) > 0 {
			// Secret detection uses the original environment, since
			// Interpolate merges the pipeline's env block into `environ`.
			searchForSecrets(l, &cfg, environ.Dump(), result, src)
		}

		if cfg.JWKSFile != "" {
			key, err := jwkutil.LoadKey(cfg.JWKSFile, cfg.JWKSKeyID)
			if err != nil {
				return fmt.Errorf("couldn't read the signing key file: %w", err)
			}

			if err := signature.SignPipeline(result, key, os.Getenv("BUILDKITE_REPO")); err != nil {
				return fmt.Errorf("couldn't sign pipeline: %w", err)
			}
		}

		// In dry-run mode we just output the generated pipeline to stdout.
		if cfg.DryRun {
			var encode func(any) error

			switch cfg.DryRunFormat {
			case "json":
				enc := json.NewEncoder(c.App.Writer)
				enc.SetIndent("", "  ")
				encode = enc.Encode

			case "yaml":
				encode = yaml.NewEncoder(c.App.Writer).Encode

			default:
				return fmt.Errorf("unknown output format %q", cfg.DryRunFormat)
			}

			// All logging happens to stderr.
			// So this can be used with other tools to get interpolated, signed
			// JSON or YAML.
			if err := encode(result); err != nil {
				return err
			}

			return nil
		}

		// Check we have a job id set if not in dry run
		if cfg.Job == "" {
			return errors.New("missing job parameter. Usually this is set in the environment for a Buildkite job via BUILDKITE_JOB_ID.")
		}

		// Check we have an agent access token if not in dry run
		if cfg.AgentAccessToken == "" {
			return errors.New("missing agent-access-token parameter. Usually this is set in the environment for a Buildkite job via BUILDKITE_AGENT_ACCESS_TOKEN.")
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
			return err
		}

		l.Info("Successfully uploaded and parsed pipeline config")

		return nil
	},
}

func searchForSecrets(
	l logger.Logger,
	cfg *PipelineUploadConfig,
	environ map[string]string,
	result *pipeline.Pipeline,
	src string,
) error {
	// Get vars to redact, as both a map and a slice.
	vars := redact.Vars(shell.StderrLogger, cfg.RedactedVars, environ)
	needles := make([]string, 0, len(vars))
	for _, needle := range vars {
		needles = append(needles, needle)
	}

	// Use a streaming replacer as a string searcher.
	secretsFound := make([]string, 0, len(needles))
	searcher := replacer.New(io.Discard, needles, func(found []byte) []byte {
		// It matched some of the needles, but which ones?
		// (This information could be plumbed through the replacer, if
		// we wanted to make it even more complicated.)
		for needleKey, needle := range vars {
			if strings.Contains(string(found), needle) {
				secretsFound = append(secretsFound, needleKey)
			}
		}
		return nil
	})

	// Encode the pipeline as JSON into the searcher.
	if err := json.NewEncoder(searcher).Encode(result); err != nil {
		return fmt.Errorf("couldn’t scan the %q pipeline for redacted variables. This parsed pipeline could not be serialized, ensure the pipeline YAML is valid, or ignore interpolated secrets for this upload by passing --redacted-vars=''. (%w)", src, err)
	}

	if len(secretsFound) > 0 {
		if cfg.RejectSecrets {
			return fmt.Errorf("pipeline %q contains values interpolated from the following secret environment variables: %v, and cannot be uploaded to Buildkite", src, secretsFound)
		}

		l.Warn("Pipeline %q contains values interpolated from the following secret environment variables: %v, which could leak sensitive information into the Buildkite UI.", src, secretsFound)
		l.Warn("This pipeline will still be uploaded, but if you'd like to to prevent this from happening, you can use the `--reject-secrets` cli flag, or the `BUILDKITE_AGENT_PIPELINE_UPLOAD_REJECT_SECRETS` environment variable, which will make the `buildkite-agent pipeline upload` command fail if it finds secrets in the pipeline.")
		l.Warn("The behaviour in the above flags will become default in Buildkite Agent v4")
	}

	return nil
}

func (cfg *PipelineUploadConfig) parseAndInterpolate(src string, input io.Reader, environ *env.Environment) (*pipeline.Pipeline, error) {
	result, err := pipeline.Parse(input)
	if err != nil && !warning.Is(err) {
		return nil, fmt.Errorf("pipeline parsing of %q failed: %w", src, err)
	}
	if !cfg.NoInterpolation {
		if tracing, has := environ.Get(tracetools.EnvVarTraceContextKey); has {
			if result.Env == nil {
				result.Env = ordered.NewMap[string, string](1)
			}
			result.Env.Set(tracetools.EnvVarTraceContextKey, tracing)
		}
		if err := result.Interpolate(environ, false); err != nil {
			return nil, fmt.Errorf("pipeline interpolation of %q failed: %w", src, err)
		}
	}
	// err may be nil or a warning from pipeline.Parse
	return result, err
}
