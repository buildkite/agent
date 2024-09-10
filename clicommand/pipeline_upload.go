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
	"slices"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/buildkite/agent/v3/agent"
	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/internal/awslib"
	awssigner "github.com/buildkite/agent/v3/internal/cryptosigner/aws"
	"github.com/buildkite/agent/v3/internal/experiments"
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
	"github.com/buildkite/interpolate"
	"github.com/urfave/cli"
	"golang.org/x/exp/maps"
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
	JWKSFile         string `cli:"jwks-file"`
	JWKSKeyID        string `cli:"jwks-key-id"`
	SigningAWSKMSKey string `cli:"signing-aws-kms-key"`
	DebugSigning     bool   `cli:"debug-signing"`

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
		cli.StringFlag{
			Name:   "signing-aws-kms-key",
			Usage:  "The AWS KMS key identifier which is used to sign pipelines.",
			EnvVar: "BUILDKITE_AGENT_AWS_KMS_KEY",
		},
		cli.BoolFlag{
			Name:   "debug-signing",
			Usage:  "Enable debug logging for pipeline signing. This can potentially leak secrets to the logs as it prints each step in full before signing. Requires debug logging to be enabled",
			EnvVar: "BUILDKITE_AGENT_DEBUG_SIGNING",
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
				return fmt.Errorf("couldn't stat pipeline configuration file %q: %w", input.Name(), err)
			}
			if fi.Size() == 0 {
				return fmt.Errorf("pipeline file %q is empty", input.Name())
			}
		}

		environ := env.FromSlice(os.Environ())

		if !cfg.NoInterpolation { // yes, interpolation
			// resolve BUILDKITE_COMMIT based on the local git repo
			resolveCommit(l, environ)
		}

		src := filename
		if src == "" {
			src = "(stdin)"
		}

		result, err := cfg.parseAndInterpolate(ctx, src, input, environ)
		if err != nil {
			w := warning.As(err)
			if w == nil {
				return err
			}
			l.Warn("There were some issues with the pipeline input - pipeline upload will proceed, but might not succeed:\n%v", w)
		}

		if len(cfg.RedactedVars) > 0 {
			// Secret detection uses the original environment, since
			// Interpolate merges the pipeline's env block into `environ`.
			searchForSecrets(l, &cfg, environ, result, src)
		}

		var (
			key signature.Key
		)

		switch {
		case cfg.SigningAWSKMSKey != "":
			awscfg, err := awslib.GetConfigV2(ctx)
			if err != nil {
				return err
			}

			// assign a crypto signer which uses the KMS key to sign the pipeline
			key, err = awssigner.NewKMS(kms.NewFromConfig(awscfg), cfg.SigningAWSKMSKey)
			if err != nil {
				return fmt.Errorf("couldn't create KMS signer: %w", err)
			}

		case cfg.JWKSFile != "":
			key, err = jwkutil.LoadKey(cfg.JWKSFile, cfg.JWKSKeyID)
			if err != nil {
				return fmt.Errorf("couldn't read the signing key file: %w", err)
			}
		}

		if key != nil {

			err = signature.SignSteps(
				ctx,
				result.Steps,
				key,
				os.Getenv("BUILDKITE_REPO"),
				signature.WithEnv(result.Env.ToMap()),
				signature.WithLogger(l),
				signature.WithDebugSigning(cfg.DebugSigning),
			)
			if err != nil {
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

// resolveCommit resolves and replaces BUILDKITE_COMMIT with the resolved value.
func resolveCommit(l logger.Logger, environ *env.Environment) {
	commitRef, has := environ.Get("BUILDKITE_COMMIT")
	if !has {
		return
	}
	cmdOut, err := exec.Command("git", "rev-parse", commitRef).Output()
	if err != nil {
		l.Warn("Error running git rev-parse %q: %v", commitRef, err)
		return
	}
	trimmedCmdOut := strings.TrimSpace(string(cmdOut))
	l.Info("Updating BUILDKITE_COMMIT to %q", trimmedCmdOut)
	environ.Set("BUILDKITE_COMMIT", trimmedCmdOut)
}

// allEnvVars recursively iterates env vars from any object that contains them:
// the pipeline itself, or command steps (which may be nested inside group
// steps).
func allEnvVars(o any, f func(p env.Pair)) {
	switch x := o.(type) {
	case *pipeline.Pipeline:
		// First iterate through all pipeline env vars.
		x.Env.Range(func(k, v string) error {
			f(env.Pair{Name: k, Value: v})
			return nil
		})
		// Recurse through each step.
		for _, s := range x.Steps {
			allEnvVars(s, f)
		}

	case *pipeline.CommandStep:
		// Iterate through the step env vars.
		for n, v := range x.Env {
			f(env.Pair{Name: n, Value: v})
		}

	case *pipeline.GroupStep:
		// Recurse through each step.
		for _, s := range x.Steps {
			allEnvVars(s, f)
		}
	}
}

// isPureSubstitution reports whether value is a shell variable substitution
// without any default or surrounding text.
func isPureSubstitution(value string) bool {
	if !strings.HasPrefix(value, "$") {
		return false
	}
	interp, err := interpolate.Interpolate(nil, value)
	if err != nil {
		// Wasn't a valid substitution.
		return false
	}
	// If it was purely a substitution, then the interpolation result using an
	// empty env should be empty.
	return interp == ""
}

func searchForSecrets(
	l logger.Logger,
	cfg *PipelineUploadConfig,
	environ *env.Environment,
	pp *pipeline.Pipeline,
	src string,
) error {
	secretsFound := make(map[string]struct{})
	shortValues := make(map[string]struct{})

	// The pipeline being uploaded can also contain secret-shaped environment
	// variables in the env maps strewn throughout the pipeline (pipeline env
	// and step env).
	// Just because it's a variable written in the pipeline YAML, doesn't mean
	// it's not a secret that needs rejecting from the upload!
	var allVars []env.Pair
	allEnvVars(pp, func(pair env.Pair) {
		// Variables declared within the pipeline might look like this after
		// interpolation:
		//
		//   env:
		//     MY_SECRET: $RUNTIME_SECRET
		//
		// MY_SECRET is actually defined at job runtime, not now, and its
		// value is not currently known. So it's not a secret. We can skip it.
		if isPureSubstitution(pair.Value) {
			return
		}
		allVars = append(allVars, pair)
	})

	// Unlike env vars from the env, we know these exist in the pipeline YAML!
	// So we can declare the secrets to be found if they match the usual rules.
	matched, short, err := redact.Vars(cfg.RedactedVars, allVars)
	if err != nil {
		l.Warn("Couldn't match environment variable names against redacted-vars: %v", err)
	}
	for _, name := range short {
		shortValues[name] = struct{}{}
	}
	for _, pair := range matched {
		secretsFound[pair.Name] = struct{}{}
	}

	// Now consider env vars from the environment.
	// Filter these down to the vars normally redacted.
	matched, short, err = redact.Vars(cfg.RedactedVars, environ.DumpPairs())
	if err != nil {
		l.Warn("Couldn't match environment variable names against redacted-vars: %v", err)
	}
	for _, name := range short {
		shortValues[name] = struct{}{}
	}

	// Create a slice of values to search the pipeline for.
	needles := make([]string, 0, len(matched))
	for _, pair := range matched {
		needles = append(needles, pair.Value)
	}

	// Use a streaming replacer as a string searcher.
	searcher := replacer.New(io.Discard, needles, func(found []byte) []byte {
		// It matched some of the needles, but which ones?
		// (This information could be plumbed through the replacer, if
		// we wanted to make it even more complicated.)
		for _, pair := range matched {
			if strings.Contains(string(found), pair.Value) {
				secretsFound[pair.Name] = struct{}{}
			}
		}
		return nil
	})

	// Encode the pipeline as JSON into the searcher.
	if err := json.NewEncoder(searcher).Encode(pp); err != nil {
		return fmt.Errorf("couldn’t scan the %q pipeline for redacted variables. This parsed pipeline could not be serialized, ensure the pipeline YAML is valid, or ignore interpolated secrets for this upload by passing --redacted-vars=''. (%w)", src, err)
	}

	if len(shortValues) > 0 {
		vars := maps.Keys(shortValues)
		slices.Sort(vars)
		l.Warn("Some variables have values below minimum length (%d bytes) and will not be redacted: %s", redact.LengthMin, strings.Join(vars, ", "))
	}

	if len(secretsFound) > 0 {
		secretsFound := maps.Keys(secretsFound)
		slices.Sort(secretsFound)

		if cfg.RejectSecrets {
			return fmt.Errorf("pipeline %q contains values interpolated from the following secret environment variables: %v, and cannot be uploaded to Buildkite", src, secretsFound)
		}

		l.Warn("Pipeline %q contains values interpolated from the following secret environment variables: %v, which could leak sensitive information into the Buildkite UI.", src, secretsFound)
		l.Warn("This pipeline will still be uploaded, but if you'd like to to prevent this from happening, you can use the `--reject-secrets` cli flag, or the `BUILDKITE_AGENT_PIPELINE_UPLOAD_REJECT_SECRETS` environment variable, which will make the `buildkite-agent pipeline upload` command fail if it finds secrets in the pipeline.")
		l.Warn("The behaviour in the above flags will become default in Buildkite Agent v4")
	}

	return nil
}

func (cfg *PipelineUploadConfig) parseAndInterpolate(ctx context.Context, src string, input io.Reader, environ *env.Environment) (*pipeline.Pipeline, error) {
	result, err := pipeline.Parse(input)
	if err != nil && !warning.Is(err) {
		return nil, fmt.Errorf("pipeline parsing of %q failed: %w", src, err)
	}
	if cfg.NoInterpolation {
		// Note that err may be nil or a non-nil warning from pipeline.Parse
		return result, err
	}
	// Pass the trace context from our environment to the pipeline.
	if tracing, has := environ.Get(tracetools.EnvVarTraceContextKey); has {
		if result.Env == nil {
			result.Env = ordered.NewMap[string, string](1)
		}
		result.Env.Set(tracetools.EnvVarTraceContextKey, tracing)
	}
	// Do the interpolation.
	preferRuntimeEnv := experiments.IsEnabled(ctx, experiments.InterpolationPrefersRuntimeEnv)
	if err := result.Interpolate(environ, preferRuntimeEnv); err != nil {
		return nil, fmt.Errorf("pipeline interpolation of %q failed: %w", src, err)
	}
	return result, err
}
