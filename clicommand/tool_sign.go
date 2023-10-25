package clicommand

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/buildkite/agent/v3/internal/pipeline"
	"github.com/buildkite/agent/v3/internal/stdin"
	"github.com/urfave/cli"
	"gopkg.in/yaml.v3"
)

type ToolSignConfig struct {
	FilePath string `cli:"arg:0" label:"upload paths"`

	JWKSFilePath string `cli:"jwks-file-path"`
	SigningKeyID string `cli:"signing-key-id"`

	// Pipeline invariants
	OrganizationSlug string `cli:"organization-slug"`
	PipelineSlug     string `cli:"pipeline-slug"`
	Repository       string `cli:"repo"`

	// Global flags
	Debug       bool     `cli:"debug"`
	LogLevel    string   `cli:"log-level"`
	NoColor     bool     `cli:"no-color"`
	Experiments []string `cli:"experiment" normalize:"list"`
	Profile     string   `cli:"profile"`
}

var ErrNoPipeline = errors.New("no pipeline file found")

var ToolSignCommand = cli.Command{
	Name:  "sign",
	Usage: "Sign pipeline steps",
	Description: `Usage:

    buildkite-agent tool sign-pipeline [options...]

Description:

This (experimental!) command takes a pipeline in YAML or JSON format as input, and annotates the
appropriate parts of the pipeline with signatures. This can then be input into the YAML steps
editor in the Buildkite UI so that the agents running these steps can verify the signatures.`,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:     "jwks-file-path",
			Usage:    "Path to a file containing a JWKS.",
			EnvVar:   "BUILDKITE_PIPELINE_UPLOAD_JWKS_FILE_PATH",
			Required: true,
		},
		cli.StringFlag{
			Name:     "signing-key-id",
			Usage:    "The JWKS key ID to use when signing the pipeline.",
			EnvVar:   "BUILDKITE_PIPELINE_UPLOAD_SIGNING_KEY_ID",
			Required: true,
		},

		// Pipeline invariants
		cli.StringFlag{
			Name:     "organization-slug",
			Usage:    "The organization slug to use when signing the pipeline.",
			EnvVar:   "BUILDKITE_ORGANIZATION_SLUG",
			Required: true,
		},
		cli.StringFlag{
			Name:     "pipeline-slug",
			Usage:    "The pipeline slug to use when signing the pipeline.",
			EnvVar:   "BUILDKITE_PIPELINE_SLUG",
			Required: true,
		},
		cli.StringFlag{
			Name:     "repo",
			Usage:    "The repository to use when signing the pipeline.",
			EnvVar:   "BUILDKITE_REPO",
			Required: true,
		},

		// Global flags
		NoColorFlag,
		DebugFlag,
		LogLevelFlag,
		ExperimentsFlag,
		ProfileFlag,
	},

	Action: func(c *cli.Context) error {
		_, cfg, l, _, done := setupLoggerAndConfig[ToolSignConfig](context.Background(), c)
		defer done()

		// Find the pipeline either from STDIN or the first argument
		var input *os.File
		var filename string

		switch {
		case cfg.FilePath != "":
			l.Info("Reading pipeline config from %q", cfg.FilePath)

			file, err := os.Open(cfg.FilePath)
			if err != nil {
				return fmt.Errorf("failed to read file: %w", err)
			}
			defer file.Close()

			input = file
			filename = cfg.FilePath

		case stdin.IsReadable():
			l.Info("Reading pipeline config from STDIN")

			// Actually read the file from STDIN
			input = os.Stdin
			filename = "(stdin)"

		default:
			return ErrNoPipeline
		}

		// Parse the pipeline
		result, err := pipeline.Parse(input)
		if err != nil {
			return fmt.Errorf("pipeline parsing of %q failed: %v", filename, err)
		}

		l.Debug("Pipeline parsed successfully: %v", result)

		pInv := &pipeline.PipelineInvariants{
			OrganizationSlug: cfg.OrganizationSlug,
			PipelineSlug:     cfg.PipelineSlug,
			Repository:       cfg.Repository,
		}

		key, err := loadSigningKey(&cfg)
		if err != nil {
			return fmt.Errorf("couldn't read the signing key file: %w", err)
		}

		if err := result.Sign(key, pInv); err != nil {
			return fmt.Errorf("couldn't sign pipeline: %w", err)
		}

		enc := yaml.NewEncoder(c.App.Writer)
		enc.SetIndent(2)
		return enc.Encode(result)
	},
}

func (cfg *ToolSignConfig) jwksFilePath() string {
	return cfg.JWKSFilePath
}

func (cfg *ToolSignConfig) signingKeyId() string {
	return cfg.SigningKeyID
}
