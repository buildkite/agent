package clicommand

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"drjosh.dev/zzglob"
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

const ifChangedSkippedMsg = "if_changed matched no unexcluded changed files in this build"

const defaultGitDiffBase = "origin/main"

type PipelineUploadConfig struct {
	GlobalConfig
	APIConfig

	FilePaths       []string `cli:"arg:*" label:"upload paths"`
	Replace         bool     `cli:"replace"`
	Job             string   `cli:"job"` // required, but not in dry-run mode
	DryRun          bool     `cli:"dry-run"`
	DryRunFormat    string   `cli:"format"`
	NoInterpolation bool     `cli:"no-interpolation"`
	RedactedVars    []string `cli:"redacted-vars" normalize:"list"`
	RejectSecrets   bool     `cli:"reject-secrets"`

	// Used for if_changed processing
	ApplyIfChanged   bool   `cli:"apply-if-changed"`
	GitDiffBase      string `cli:"git-diff-base"`
	FetchDiffBase    bool   `cli:"fetch-diff-base"`
	ChangedFilesPath string `cli:"changed-files-path"`

	// Used for signing
	JWKSFile         string `cli:"jwks-file"`
	JWKSKeyID        string `cli:"jwks-key-id"`
	SigningAWSKMSKey string `cli:"signing-aws-kms-key"`
	DebugSigning     bool   `cli:"debug-signing"`
}

var PipelineUploadCommand = cli.Command{
	Name:        "upload",
	Usage:       "Uploads a description of a build pipeline adds it to the currently running build after the current job",
	Description: pipelineUploadHelpDescription,
	Flags: slices.Concat(globalFlags(), apiFlags(), []cli.Flag{
		cli.BoolFlag{
			Name:   "replace",
			Usage:  "Replace the rest of the existing pipeline with the steps uploaded. Jobs that are already running are not removed (default: false)",
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
			Usage:  "Rather than uploading the pipeline, it will be echoed to stdout (default: false)",
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
			Usage:  "Skip variable interpolation into the pipeline prior to upload (default: false)",
			EnvVar: "BUILDKITE_PIPELINE_NO_INTERPOLATION",
		},
		cli.BoolFlag{
			Name:   "reject-secrets",
			Usage:  "When true, fail the pipeline upload early if the pipeline contains secrets (default: false)",
			EnvVar: "BUILDKITE_AGENT_PIPELINE_UPLOAD_REJECT_SECRETS",
		},
		cli.BoolTFlag{
			Name:   "apply-if-changed",
			Usage:  "When enabled, steps containing an ′if_changed′ key are evaluated against the git diff. If the ′if_changed′ glob pattern match no files changed in the build, the step is skipped. Minimum Buildkite Agent version: v3.99 (with --apply-if-changed flag), v3.103.0 (enabled by default) (default: true)",
			EnvVar: "BUILDKITE_AGENT_APPLY_IF_CHANGED,BUILDKITE_AGENT_APPLY_SKIP_IF_UNCHANGED",
		},
		cli.StringFlag{
			Name:   "git-diff-base",
			Usage:  "Provides the base from which to find the git diff when processing ′if_changed′, e.g. origin/main. If not provided, it uses the first valid value of {origin/$BUILDKITE_PULL_REQUEST_BASE_BRANCH, origin/$BUILDKITE_PIPELINE_DEFAULT_BRANCH, origin/main}.",
			EnvVar: "BUILDKITE_GIT_DIFF_BASE",
		},
		cli.BoolFlag{
			Name:   "fetch-diff-base",
			Usage:  "When enabled, the base for computing the git diff will be git-fetched prior to computing the diff (default: false)",
			EnvVar: "BUILDKITE_FETCH_DIFF_BASE",
		},
		cli.StringFlag{
			Name:   "changed-files-path",
			Usage:  "Path to a file containing the list of changed files (newline-separated) to use for ′if_changed′ evaluation. When provided, the agent skips running git commands to determine changed files.",
			EnvVar: "BUILDKITE_CHANGED_FILES_PATH",
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
			Usage:  "Enable debug logging for pipeline signing. This can potentially leak secrets to the logs as it prints each step in full before signing. Requires debug logging to be enabled (default: false)",
			EnvVar: "BUILDKITE_AGENT_DEBUG_SIGNING",
		},
		RedactedVars,
	}),
	Action: func(c *cli.Context) error {
		ctx := context.Background()
		ctx, cfg, l, _, done := setupLoggerAndConfig[PipelineUploadConfig](ctx, c)
		defer done()

		// Find the pipeline either from STDIN or the non-flag arguments
		type input struct {
			file *os.File
			name string
		}
		var inputs []input

		switch {
		case len(cfg.FilePaths) > 0:
			l.Info("Reading pipeline configs from %q", cfg.FilePaths)

			for _, fn := range cfg.FilePaths {
				file, err := os.Open(fn)
				if err != nil {
					return fmt.Errorf("failed to read file: %w", err)
				}
				defer file.Close()
				inputs = append(inputs, input{file, filepath.Base(fn)})
			}

		case stdin.IsReadable():
			l.Info("Reading pipeline config from STDIN")

			// Actually read the file from STDIN
			inputs = []input{{os.Stdin, "(stdin)"}}

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
			file, err := os.Open(found)
			if err != nil {
				return fmt.Errorf("failed to read file %q: %w", found, err)
			}
			defer file.Close()
			inputs = []input{{file, filepath.Base(found)}}
		}

		// Make sure each file (other than stdin) actually has something in it
		for _, input := range inputs {
			if input.file == os.Stdin {
				continue
			}
			fi, err := input.file.Stat()
			if err != nil {
				return fmt.Errorf("couldn't stat pipeline configuration file %q: %w", input.file.Name(), err)
			}
			if fi.Size() == 0 {
				return fmt.Errorf("pipeline file %q is empty", input.file.Name())
			}
		}

		environ := env.FromSlice(os.Environ())

		if !cfg.NoInterpolation { // yes, interpolation
			// resolve BUILDKITE_COMMIT based on the local git repo
			resolveCommit(l, environ)
		}

		// Used to encode output in dry-run mode.
		dryRunEnc := func(any) error { return nil }
		if cfg.DryRun {
			switch cfg.DryRunFormat {
			case "json":
				enc := json.NewEncoder(c.App.Writer)
				enc.SetIndent("", "  ")
				dryRunEnc = enc.Encode

			case "yaml":
				dryRunEnc = yaml.NewEncoder(c.App.Writer).Encode

			default:
				return fmt.Errorf("unknown output format %q", cfg.DryRunFormat)
			}
		}

		prependOriginIfNonempty := func(key string) string {
			s := os.Getenv(key)
			if s == "" {
				return ""
			}
			return "origin/" + s
		}

		ifChanged := &ifChangedApplicator{
			enabled: cfg.ApplyIfChanged,
			diffBase: cmp.Or(
				cfg.GitDiffBase,
				prependOriginIfNonempty("BUILDKITE_PULL_REQUEST_BASE_BRANCH"),
				prependOriginIfNonempty("BUILDKITE_PIPELINE_DEFAULT_BRANCH"),
				defaultGitDiffBase,
			),
			changedFilesPath: cfg.ChangedFilesPath,
		}

		// Process all inputs.
		for _, input := range inputs {

			// For each pipeline in the input (could be multiple)...
			count := 1
			for result, err := range cfg.parseAndInterpolate(ctx, input.name, input.file, environ) {
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
					err := searchForSecrets(l, &cfg, environ, result, input.name)
					if err != nil {
						return NewExitError(1, err)
					}
				}

				var key signature.Key

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
					err := signature.SignSteps(
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

				// Apply or strip out `if_changed`, based on settings.
				ifChanged.apply(l, result.Steps)

				// All logging happens to stderr.
				// So this can be used with other tools to get interpolated, signed
				// JSON or YAML.
				if err := dryRunEnc(result); err != nil {
					return err
				}

				// In dry-run mode we just output the generated pipeline to stdout,
				// and don't want to upload it.
				if cfg.DryRun {
					continue
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

				l.Info("Successfully parsed and uploaded pipeline #%d from %q", count, input.name)
				count++
			}
		}

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
		vars := slices.Collect(maps.Keys(shortValues))
		slices.Sort(vars)
		l.Warn("Some variables have values below minimum length (%d bytes) and will not be redacted: %s", redact.LengthMin, strings.Join(vars, ", "))
	}

	if len(secretsFound) > 0 {
		secretsFound := slices.Collect(maps.Keys(secretsFound))
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

func (cfg *PipelineUploadConfig) parseAndInterpolate(ctx context.Context, src string, input io.Reader, environ *env.Environment) iter.Seq2[*pipeline.Pipeline, error] {
	return func(yield func(*pipeline.Pipeline, error) bool) {
		for result, err := range pipeline.ParseAll(input) {
			// Check the error, apply interpolation if needed.
			switch {
			case err != nil && !warning.Is(err):
				err = fmt.Errorf("pipeline parsing of %q failed: %w", src, err)
				// yield below

			case cfg.NoInterpolation:
				// Note that err may be nil or a non-nil warning from pipeline.Parse
				// yield below

			default: // yes, interpolation
				// Pass the trace context from our environment to the pipeline.
				if tracing, has := environ.Get(tracetools.EnvVarTraceContextKey); has {
					if result.Env == nil {
						result.Env = ordered.NewMap[string, string](1)
					}
					result.Env.Set(tracetools.EnvVarTraceContextKey, tracing)
				}

				// Do the interpolation.
				preferRuntimeEnv := experiments.IsEnabled(ctx, experiments.InterpolationPrefersRuntimeEnv)
				err = result.Interpolate(environ, preferRuntimeEnv)
				if err != nil {
					err = fmt.Errorf("pipeline interpolation of %q failed: %w", src, err)
				}
				// yield below
			}

			// Yield the result and error.
			if !yield(result, err) {
				return
			}
		}
	}
}

// readChangedFilesFromPath reads a newline-separated list of changed files from a file.
func readChangedFilesFromPath(l logger.Logger, path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading changed files from %q: %w", path, err)
	}
	lines := strings.Split(string(data), "\n")
	// Filter out empty lines
	changedPaths := slices.DeleteFunc(lines, func(s string) bool {
		return strings.TrimSpace(s) == ""
	})
	plural := "files"
	if len(changedPaths) == 1 {
		plural = "file"
	}
	l.Info("if_changed read %d changed %s from %q", len(changedPaths), plural, path)
	return changedPaths, nil
}

// gatherChangedFiles determines changed files in this build.
func gatherChangedFiles(l logger.Logger, diffBase string) (changedPaths []string, err error) {
	// Corporate needs you to find the differences between diffBase and HEAD.
	diffBaseCommit, err := exec.Command("git", "rev-parse", diffBase).Output()
	if err != nil {
		return nil, gitRevParseError{arg: diffBase, wrapped: err}
	}
	headCommit, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		return nil, gitRevParseError{arg: "HEAD", wrapped: err}
	}
	if strings.TrimSpace(string(diffBaseCommit)) == strings.TrimSpace(string(headCommit)) {
		// They're the same commit, so `git diff` will (correctly) report no
		// changes between them.
		// This often happens for builds triggered on main, when:
		// * a PR is merged. HEAD is hopefully either a merge commit or a
		//   squash commit.
		// * the developers commit and push directly to main.
		// As a heuristic, we look at changed files between the latest commit
		// and its parent (the first parent, if it is a merge commit).
		// If _multiple_ commits were pushed at once, then this approach will
		// miss changes from earlier commits. Thus, log a warning.
		l.Warn("Applying if_changed conditions relative to the first parent of HEAD (because HEAD = %q)", diffBase)
		l.Warn("If this build is intended to include more than one commit on this branch, if_changed may calculate an incomplete diff. You may need to adjust the --git-diff-base flag or BUILDKITE_GIT_DIFF_BASE env var to choose a different base commit for calculating diffs.")

		// Flag Explainer:
		// `--first-parent`: when reaching a merge commit, follow the first parent
		// (the branch that was merged *into*). Implies --diff-merges=first-parent,
		// which is important for turning merge commits into changes.
		// `-1`: show only the most recent entry from the log
		// `--name-only`: ensure the names of each changed file are printed
		// `--pretty='format:'`: only print the names of the files.
		gitLog, err := exec.Command("git", "log", "--first-parent", "-1", "--name-only", "--pretty=format:").Output()
		if err != nil {
			return nil, gitLogError{wrapped: err}
		}
		changedPaths = strings.Split(string(gitLog), "\n")
	} else {
		// The --merge-base flag was only added to git-diff recently.
		mergeBaseOut, err := exec.Command("git", "merge-base", diffBase, "HEAD").Output()
		if err != nil {
			return nil, gitMergeBaseError{diffBase: diffBase, wrapped: err}
		}
		mergeBase := strings.TrimSpace(string(mergeBaseOut))
		l.Info("Applying if_changed conditions relative to %q (the merge-base of %q and HEAD)", mergeBase, diffBase)

		gitDiff, err := exec.Command("git", "diff", "--name-only", mergeBase).Output()
		if err != nil {
			return nil, gitDiffError{mergeBase: mergeBase, wrapped: err}
		}
		changedPaths = strings.Split(string(gitDiff), "\n")
	}
	changedPaths = slices.DeleteFunc(changedPaths, func(s string) bool {
		return strings.TrimSpace(s) == ""
	})
	plural := "files"
	if len(changedPaths) == 1 {
		plural = "file"
	}
	l.Info("if_changed found %d changed %s", len(changedPaths), plural)
	return changedPaths, nil
}

type gitRevParseError struct {
	arg     string
	wrapped error
}

func (e gitRevParseError) Error() string {
	return fmt.Sprintf("git rev-parse %q: %v", e.arg, e.wrapped)
}

func (e gitRevParseError) Unwrap() error { return e.wrapped }

type gitLogError struct {
	wrapped error
}

func (e gitLogError) Error() string {
	return fmt.Sprintf("git log --first-parent -1 --name-only --pretty=\"format:\": %v", e.wrapped)
}

func (e gitLogError) Unwrap() error { return e.wrapped }

type gitMergeBaseError struct {
	diffBase string
	wrapped  error
}

func (e gitMergeBaseError) Error() string {
	return fmt.Sprintf("git merge-base %q HEAD: %v", e.diffBase, e.wrapped)
}
func (e gitMergeBaseError) Unwrap() error { return e.wrapped }

type gitDiffError struct {
	mergeBase string
	wrapped   error
}

func (e gitDiffError) Error() string {
	return fmt.Sprintf("git diff --name-only %q: %v", e.mergeBase, e.wrapped)
}
func (e gitDiffError) Unwrap() error { return e.wrapped }

// ifChangedApplicator applies `if_changed` as it appears within the pipeline
// being uploaded. The `if_changed` attribute takes a glob pattern of files
// to match. The step is skipped if the glob doesn't match any "changed files".
type ifChangedApplicator struct {
	enabled          bool // apply-if-changed is enabled
	gathered         bool // the changed files have been computed?
	diffBase         string
	changedFilesPath string // path to a file containing newline-separated changed files
	changedPaths     []string
}

// apply applies "if_changed". If it's not enabled, it strips "if_changed"
// attributes. Otherwise, it converts them into "skip" if the glob
// pattern matches no changed files.
func (ica *ifChangedApplicator) apply(l logger.Logger, steps pipeline.Steps) {
stepsLoop:
	for _, step := range steps {
		// All supported step types store "if_changed" in a map.
		var content map[string]any

		// These step types support "skip".
		switch st := step.(type) {
		case *pipeline.GroupStep:
			// Recurse into group steps
			ica.apply(l, st.Steps)
			content = st.RemainingFields

		case *pipeline.CommandStep:
			content = st.RemainingFields

		case *pipeline.TriggerStep:
			content = st.Contents
		}

		// If content is nil then there's nothing containing an `if_changed`.
		if content == nil {
			continue
		}

		// Retrieve the if_changed value, then delete it.
		// It is not yet understood by the backend.
		ifChangedValue := content["if_changed"]
		delete(content, "if_changed")

		// If there's no if_changed, then the step is unconditional.
		if ifChangedValue == nil {
			continue
		}

		if !ica.enabled {
			// Not applying the if_changed; leaving it deleted.
			continue
		}

		// If we don't know the changed paths yet, either read from file or call out to Git.
		if !ica.gathered {
			var cps []string
			var err error

			if ica.changedFilesPath != "" {
				// Read changed files from the provided file path.
				cps, err = readChangedFilesFromPath(l, ica.changedFilesPath)
				if err != nil {
					l.Error("Couldn't read changed files from %q, not skipping any pipeline steps: %v", ica.changedFilesPath, err)
					ica.enabled = false
					continue stepsLoop
				}
			} else {
				// Determine changed files using git.
				cps, err = gatherChangedFiles(l, ica.diffBase)
				if err != nil {
					l.Error("Couldn't determine git diff from upstream, not skipping any pipeline steps: %v", err)
					var exitErr *exec.ExitError
					if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
						// stderr came from git, which is typically human readable
						l.Error("git: %s", exitErr.Stderr)
					}
					switch err := err.(type) {
					case gitRevParseError:
						l.Error("This could be because %q might not be a commit in the repository.\n"+
							"You may need to change the --git-diff-base flag or BUILDKITE_GIT_DIFF_BASE env var.",
							err.arg,
						)

					case gitMergeBaseError:
						l.Error("This could be because %q might not be a commit in the repository.\n"+
							"You may need to change the --git-diff-base flag or BUILDKITE_GIT_DIFF_BASE env var.",
							err.diffBase,
						)

					case gitDiffError:
						l.Error("This could be because the merge-base that Git found, %q, might be invalid.\n"+
							"You may need to change the --git-diff-base flag or BUILDKITE_GIT_DIFF_BASE env var.",
							err.mergeBase,
						)
					}

					// Because changed files couldn't be determined, we switch into
					// disabled mode.
					ica.enabled = false
					continue stepsLoop
				}
			}

			// The changed files are now known.
			ica.gathered = true
			ica.changedPaths = cps
		}

		var include, exclude []*zzglob.Pattern
		switch x := ifChangedValue.(type) {
		case *ordered.MapSA:
			// Object form:
			// if_changed:
			//   include: (required; string or list)
			//   exclude: (optional; string or list)
			inclVal, has := x.Get("include")
			if !has {
				l.Warn("The value for if_changed was a mapping, but it didn't have an `include` key. The step will not be skipped.")
				continue stepsLoop
			}
			var err error
			include, err = ifChangedPatterns(inclVal)
			if err != nil {
				l.Warn("Couldn't parse if_changed.include patterns: %v. The step will not be skipped.", err)
				continue stepsLoop
			}
			exclVal, has := x.Get("exclude")
			if !has {
				break // switch
			}
			exclude, err = ifChangedPatterns(exclVal)
			if err != nil {
				l.Warn("Couldn't parse if_changed.exclude patterns: %v. The step will not be skipped.", err)
				continue stepsLoop
			}

		default:
			// Should be either a simple string or a list of strings.
			inc, err := ifChangedPatterns(x)
			if err != nil {
				l.Warn("Couldn't parse if_changed patterns: %v. The step will not be skipped.", err)
				continue stepsLoop
			}
			include = inc
		}

		// For each remaining changed path, test it against each pattern in
		// `exclude` and `include`.
		// If it's included, and wasn't excluded, we _don't skip_ this step.
		// Got it?
	pathsLoop:
		for _, cp := range ica.changedPaths {
			// First, check if the path is excluded.
			for _, g := range exclude {
				if g.Match(cp) {
					continue pathsLoop
				}
			}
			// It's not excluded! now check if it is included.
			for _, g := range include {
				if g.Match(cp) {
					continue stepsLoop
				}
			}
		}

		// The globs matched no changed file paths - this step is now skipped.
		// Note that the "skip" string is limited to 70 characters.
		content["skip"] = ifChangedSkippedMsg
	}
}

// ifChangedPatterns converts a string or list within `if_changed` into a slice
// of parsed globs.
func ifChangedPatterns(value any) ([]*zzglob.Pattern, error) {
	if value == nil {
		return nil, nil
	}

	var patterns []string
	switch x := value.(type) {
	case string:
		// A single string.
		patterns = []string{x}

	case []any:
		// A list of strings.
		for i, item := range x {
			patt, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("at index %d, the item had unsupported type %T", i, item)
			}
			patterns = append(patterns, patt)
		}

	default:
		return nil, fmt.Errorf("the value had unsupported type %T", x)
	}

	globs := make([]*zzglob.Pattern, 0, len(patterns))
	for _, patt := range patterns {
		g, err := zzglob.Parse(patt)
		if err != nil {
			return nil, fmt.Errorf("while parsing glob pattern %q: %w", patt, err)
		}
		globs = append(globs, g)
	}

	return globs, nil
}
