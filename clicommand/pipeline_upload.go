package clicommand

import (
	"context"
	"encoding/json"
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
	"github.com/buildkite/agent/v3/internal/pipeline"
	"github.com/buildkite/agent/v3/internal/redact"
	"github.com/buildkite/agent/v3/internal/replacer"
	"github.com/buildkite/agent/v3/internal/stdin"
	"github.com/buildkite/agent/v3/logger"
	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/urfave/cli"
	"golang.org/x/exp/slices"
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

	JWKSFilePath     string `cli:"jwks-file-path"`
	SigningKeyID     string `cli:"signing-key-id"`
	SigningAlgorithm string `cli:"signing-algorithm"`

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
			Usage:  "Skip variable interpolation the pipeline when uploaded",
			EnvVar: "BUILDKITE_PIPELINE_NO_INTERPOLATION",
		},
		cli.BoolFlag{
			Name:   "reject-secrets",
			Usage:  "When true, fail the pipeline upload early if the pipeline contains secrets",
			EnvVar: "BUILDKITE_AGENT_PIPELINE_UPLOAD_REJECT_SECRETS",
		},
		cli.StringFlag{
			Name:   "signing-key-path",
			Usage:  "Path to a file containing a signing key. Passing this flag enables pipeline signing. For hmac-sha256, the raw file content is used as the shared key",
			EnvVar: "BUILDKITE_PIPELINE_UPLOAD_SIGNING_KEY_PATH",
		},
		cli.StringFlag{
			Name:   "jwks-file-path",
			Usage:  "Path to a file containing a JWKS. Passing this flag enables pipeline signing",
			EnvVar: "BUILDKITE_PIPELINE_UPLOAD_JWKS_FILE_PATH",
		},
		cli.StringFlag{
			Name:   "signing-key-id",
			Usage:  "The JWKS key ID to use when signing the pipeline. Required when using a JWKS",
			EnvVar: "BUILDKITE_PIPELINE_UPLOAD_SIGNING_KEY_ID",
		},
		cli.StringFlag{
			Name:   "signing-algorithm",
			Usage:  fmt.Sprintf("The algorithm to use when signing the pipeline. Must be one of %v, and valid for the given signing key. Required if the JWK specified does not have an alg key", ValidSigningAlgorithms),
			EnvVar: "BUILDKITE_PIPELINE_UPLOAD_SIGNING_ALGORITHM",
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
		cfg, l, _, done := setupLoggerAndConfig[PipelineUploadConfig](c)
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
				l.Fatal("Failed to read file: %v", err)
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
				l.Fatal("Found multiple configuration files: %s. Please only have 1 configuration file present.", strings.Join(exists, ", "))
			}
			if len(exists) == 0 {
				l.Fatal("Could not find a default pipeline configuration file. See `buildkite-agent pipeline upload --help` for more information.")
			}

			found := exists[0]

			l.Info("Found config file %q", found)

			// Read the default file
			filename = path.Base(found)
			file, err := os.Open(found)
			if err != nil {
				l.Fatal("Failed to read file %q: %v", found, err)
			}
			defer file.Close()
			input = file
		}

		// Make sure the file actually has something in it
		if input != os.Stdin {
			fi, err := input.Stat()
			if err != nil {
				l.Fatal("Couldn't stat pipeline configuration file %q: %v", input.Name(), err)
			}
			if fi.Size() == 0 {
				l.Fatal("Pipeline file %q is empty", input.Name())
			}
		}

		var environ *env.Environment
		if !cfg.NoInterpolation {
			// Load environment to pass into parser
			environ = env.FromSlice(os.Environ())

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

		// Parse the pipeline
		result, err := pipeline.Parse(input)
		if err != nil {
			l.Fatal("Pipeline parsing of %q failed: %v", src, err)
		}
		if !cfg.NoInterpolation {
			if err := result.Interpolate(environ); err != nil {
				l.Fatal("Pipeline interpolation of %q failed: %v", src, err)
			}
		}

		if len(cfg.RedactedVars) > 0 {
			// Secret detection uses the original environment, since
			// Interpolate merges the pipeline's env block into `environ`.
			envMap := env.FromSlice(os.Environ()).Dump()
			searchForSecrets(l, &cfg, envMap, result, src)
		}

		if cfg.JWKSFilePath != "" {
			l.Warn("Pipeline signing is experimental and the user interface might change! Also it might not work, it might sign the pipeline only partially, or it might eat your pet dog. You have been warned!")

			key, err := loadSigningKey(cfg)
			if err != nil {
				l.Fatal("Couldn't load signing key: %v", err)
			}

			if err := result.Sign(key); err != nil {
				l.Fatal("Couldn't sign pipeline: %v", err)
			}
		}

		// In dry-run mode we just output the generated pipeline to stdout.
		if cfg.DryRun {
			var encode func(any) error

			switch cfg.DryRunFormat {
			case "json":
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				encode = enc.Encode

			case "yaml":
				encode = yaml.NewEncoder(os.Stdout).Encode

			default:
				l.Fatal("Unknown output format %q", cfg.DryRunFormat)
			}

			// All logging happens to stderr.
			// So this can be used with other tools to get interpolated, signed
			// JSON or YAML.
			if err := encode(result); err != nil {
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

func searchForSecrets(l logger.Logger, cfg *PipelineUploadConfig, environ map[string]string, result *pipeline.Pipeline, src string) {
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
		l.Fatal("Couldn’t scan the %q pipeline for redacted variables. This parsed pipeline could not be serialized, ensure the pipeline YAML is valid, or ignore interpolated secrets for this upload by passing --redacted-vars=''. (%s)", src, err)
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

func loadSigningKey(cfg PipelineUploadConfig) (jwk.Key, error) {
	jwksFile, err := os.Open(cfg.JWKSFilePath)
	if err != nil {
		return nil, fmt.Errorf("opening JWKS file: %v", err)
	}
	defer jwksFile.Close()

	jwksBody, err := io.ReadAll(jwksFile)
	if err != nil {
		return nil, fmt.Errorf("reading JWKS file: %v", err)
	}

	jwks, err := jwk.Parse(jwksBody)
	if err != nil {
		return nil, fmt.Errorf("parsing JWKS file: %v", err)
	}

	if cfg.SigningKeyID == "" {
		return nil, fmt.Errorf("signing key ID is required when using JWKS")
	}

	key, found := jwks.LookupKeyID(cfg.SigningKeyID)
	if !found {
		return nil, fmt.Errorf("couldn't find signing key ID %q in JWKS", cfg.SigningKeyID)
	}

	if err = injectAlgorithm(key, cfg); err != nil {
		return nil, fmt.Errorf("setting algorithm: %v", err)
	}

	if err := validateJWK(key); err != nil {
		return nil, fmt.Errorf("signing key ID %s is invalid: %v", cfg.SigningKeyID, err)
	}

	return key, nil
}

func injectAlgorithm(key jwk.Key, cfg PipelineUploadConfig) error {
	keyHasAlgorithm := key.Algorithm() != nil && key.Algorithm().String() != ""

	var alg jwa.KeyAlgorithm
	var from string // for error messages
	switch {
	case !keyHasAlgorithm && cfg.SigningAlgorithm == "":
		return fmt.Errorf("signing algorithm is required if JWKS key doesn't have an alg parameter")

	case keyHasAlgorithm && cfg.SigningAlgorithm != "":
		return fmt.Errorf("signing algorithm is not allowed if JWKS key has an alg parameter")

	case cfg.SigningAlgorithm != "":
		alg = jwa.SignatureAlgorithm(cfg.SigningAlgorithm)
		from = "from --signing-algorithm flag"

	case keyHasAlgorithm:
		alg = key.Algorithm()
		from = fmt.Sprintf("from JWKS, key ID %s", cfg.SigningKeyID)
	}

	// The above switch is exhaustive, but just in case, check that the alg isn't invalid (ie. empty)
	if _, ok := alg.(jwa.InvalidKeyAlgorithm); ok {
		return fmt.Errorf("signing algorithm %s is not a valid signing algorithm", alg)
	}

	signingAlg, ok := alg.(jwa.SignatureAlgorithm)
	if !ok {
		return fmt.Errorf("algorithm %s (%s) cannot be used for signing/verification", alg, from)
	}

	err := key.Set(jwk.AlgorithmKey, signingAlg)
	if err != nil {
		return fmt.Errorf("setting signing algorithm on key: %v", err)
	}

	return nil
}

var (
	ValidRSAAlgorithms   = []jwa.SignatureAlgorithm{jwa.PS256, jwa.PS384, jwa.PS512}
	ValidECAlgorithms    = []jwa.SignatureAlgorithm{jwa.ES256, jwa.ES384, jwa.ES512}
	ValidOctetAlgorithms = []jwa.SignatureAlgorithm{jwa.HS256, jwa.HS384, jwa.HS512}
	ValidOKPAlgorithms   = []jwa.SignatureAlgorithm{jwa.EdDSA}

	ValidSigningAlgorithms = concat(
		ValidOctetAlgorithms,
		ValidRSAAlgorithms,
		ValidECAlgorithms,
		ValidOKPAlgorithms,
	)
)

func validateJWK(key jwk.Key) error {
	validKeyTypes := []jwa.KeyType{jwa.RSA, jwa.EC, jwa.OctetSeq, jwa.OKP}
	if !slices.Contains(validKeyTypes, key.KeyType()) {
		return fmt.Errorf("unsupported key type %s. Key type must be one of %v", key.KeyType(), validKeyTypes)
	}

	signingAlg, ok := key.Algorithm().(jwa.SignatureAlgorithm)
	if !ok {
		return fmt.Errorf("key algorithm %s is not a valid signing algorithm", key.Algorithm())
	}

	validAlgsForType := map[jwa.KeyType][]jwa.SignatureAlgorithm{
		// We don't suppport RSA-PKCS1v1.5 because it's arguably less secure than RSA-PSS
		jwa.RSA:      {jwa.PS256, jwa.PS384, jwa.PS512},
		jwa.EC:       {jwa.ES256, jwa.ES384, jwa.ES512},
		jwa.OctetSeq: {jwa.HS256, jwa.HS384, jwa.HS512},
		jwa.OKP:      {jwa.EdDSA},
	}

	if !slices.Contains(validAlgsForType[key.KeyType()], signingAlg) {
		return fmt.Errorf("unsupported signing algorithm %q for key type %q. With key type %q, key algorithm must be one of %v", signingAlg, key.KeyType(), key.KeyType(), validAlgsForType[key.KeyType()])
	}

	return nil
}

func concat[T any](a ...[]T) []T {
	cap := 0
	for _, s := range a {
		cap += len(s)
	}

	var result []T
	for _, s := range a {
		result = append(result, s...)
	}
	return result
}
