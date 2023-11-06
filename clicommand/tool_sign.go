package clicommand

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/buildkite/agent/v3/internal/bkgql"
	"github.com/buildkite/agent/v3/internal/jwkutil"
	"github.com/buildkite/agent/v3/internal/pipeline"
	"github.com/buildkite/agent/v3/internal/stdin"
	"github.com/buildkite/agent/v3/logger"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/urfave/cli"
	"gopkg.in/yaml.v3"
)

type ToolSignConfig struct {
	PipelineFile string `cli:"arg:0" label:"pipeline file"`

	// These change the behaviour
	GraphQLToken string `cli:"graphql-token"`
	Update       bool   `cli:"update"`
	NoConfirm    bool   `cli:"no-confirm"`

	// Used for signing
	JWKSFile  string `cli:"jwks-file"`
	JWKSKeyID string `cli:"jwks-key-id"`

	// Needed for to use GraphQL API
	OrganizationSlug string `cli:"organization-slug"`
	PipelineSlug     string `cli:"pipeline-slug"`

	// Added to signature
	Repository string `cli:"repo"`

	// Global flags
	Debug       bool     `cli:"debug"`
	LogLevel    string   `cli:"log-level"`
	NoColor     bool     `cli:"no-color"`
	Experiments []string `cli:"experiment" normalize:"list"`
	Profile     string   `cli:"profile"`
}

const yamlIndent = 2

var (
	ErrNoPipeline = errors.New("no pipeline file found")
	ErrUseGraphQL = errors.New(
		"either provide the pipeline YAML, and the repository URL, " +
			"or provide a GraphQL token to allow them to be retrieved from Buildkite",
	)
	ErrNotFound = errors.New("pipeline not found")
)

var ToolSignCommand = cli.Command{
	Name:  "sign",
	Usage: "Sign pipeline steps",
	Description: `Usage:

    buildkite-agent tool sign-pipeline [options...]

Description:

This (experimental!) command takes a pipeline in YAML format as input, and annotates the
appropriate parts of the pipeline with signatures. This can then be input into the YAML steps
editor in the Buildkite UI so that the agents running these steps can verify the signatures.`,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:     "graphql-token",
			Usage:    "A token for the buildkite graphql API. This will be used to populate the pipeline invariants if they are not provided.",
			EnvVar:   "BUILDKITE_GRAPHQL_TOKEN",
			Required: false,
		},
		cli.BoolFlag{
			Name:   "update",
			Usage:  "Update the pipeline online after signing it. This can only be used if the GraphQL token is provided.",
			EnvVar: "BUILDKITE_TOOL_SIGN_UPDATE",
		},
		cli.BoolFlag{
			Name:   "no-confirm",
			Usage:  "Show confirmation prompts before updating the pipeline online.",
			EnvVar: "BUILDKITE_TOOL_SIGN_NO_CONFIRM",
		},

		// Used for signing
		cli.StringFlag{
			Name:     "jwks-file",
			Usage:    "Path to a file containing a JWKS.",
			EnvVar:   "BUILDKITE_AGENT_JWKS_FILE",
			Required: true,
		},
		cli.StringFlag{
			Name:     "jwks-key-id",
			Usage:    "The JWKS key ID to use when signing the pipeline. If none is provided and the JWKS file contains only one key, that key will be used.",
			EnvVar:   "BUILDKITE_AGENT_JWKS_KEY_ID",
			Required: false,
		},

		// These are required to use the GraphQL API
		cli.StringFlag{
			Name:     "organization-slug",
			Usage:    "The organization slug. Used to connect to the GraphQL API.",
			EnvVar:   "BUILDKITE_ORGANIZATION_SLUG",
			Required: false,
		},
		cli.StringFlag{
			Name:     "pipeline-slug",
			Usage:    "The pipeline slug. Used to connect to the GraphQL API.",
			EnvVar:   "BUILDKITE_PIPELINE_SLUG",
			Required: false,
		},

		// Added to signature
		cli.StringFlag{
			Name:     "repo",
			Usage:    "The URL of the pipeline's repository, which is used in the pipeline signature. If the GraphQL token is provided, this will be ignored.",
			EnvVar:   "BUILDKITE_REPO",
			Required: false,
		},

		// Global flags
		NoColorFlag,
		DebugFlag,
		LogLevelFlag,
		ExperimentsFlag,
		ProfileFlag,
	},

	Action: func(c *cli.Context) error {
		ctx, cfg, l, _, done := setupLoggerAndConfig[ToolSignConfig](context.Background(), c)
		defer done()

		l.Warn("Pipeline signing is experimental and the user interface might change!")

		key, err := jwkutil.LoadKey(cfg.JWKSFile, cfg.JWKSKeyID)
		if err != nil {
			return fmt.Errorf("couldn't read the signing key file: %w", err)
		}

		if cfg.GraphQLToken == "" {
			return signOffline(c, l, key, &cfg)
		}

		return signWithGraphQL(ctx, c, l, key, &cfg)
	},
}

func signOffline(
	c *cli.Context,
	l logger.Logger,
	key jwk.Key,
	cfg *ToolSignConfig,
) error {
	if cfg.Repository == "" {
		return ErrUseGraphQL
	}

	// Find the pipeline either from STDIN or the first argument
	var (
		input    io.Reader
		filename string
	)

	switch {
	case cfg.PipelineFile != "":
		l.Info("Reading pipeline config from %q", cfg.PipelineFile)

		file, err := os.Open(cfg.PipelineFile)
		if err != nil {
			return fmt.Errorf("failed to read file: %w", err)
		}
		defer file.Close()

		input = file
		filename = cfg.PipelineFile

	case stdin.IsReadable():
		l.Info("Reading pipeline config from STDIN")

		input = os.Stdin
		filename = "(stdin)"

	default:
		return ErrNoPipeline
	}

	parsedPipeline, err := pipeline.Parse(input)
	if err != nil {
		return fmt.Errorf("pipeline parsing of %q failed: %v", filename, err)
	}

	if cfg.Debug {
		enc := yaml.NewEncoder(c.App.Writer)
		enc.SetIndent(yamlIndent)
		if err := enc.Encode(parsedPipeline); err != nil {
			return fmt.Errorf("couldn't encode pipeline: %w", err)
		}
		l.Debug("Pipeline parsed successfully:\n%v", parsedPipeline)
	}

	if err := parsedPipeline.Sign(key, cfg.Repository); err != nil {
		return fmt.Errorf("couldn't sign pipeline: %w", err)
	}

	enc := yaml.NewEncoder(c.App.Writer)
	enc.SetIndent(yamlIndent)
	return enc.Encode(parsedPipeline)
}

func signWithGraphQL(
	ctx context.Context,
	c *cli.Context,
	l logger.Logger,
	key jwk.Key,
	cfg *ToolSignConfig,
) error {
	orgPipelineSlug := fmt.Sprintf("%s/%s", cfg.OrganizationSlug, cfg.PipelineSlug)
	debugL := l.WithFields(logger.StringField("orgPipelineSlug", orgPipelineSlug))

	l.Info("Retrieving pipeline from the GraphQL API")

	client := bkgql.NewClient(cfg.GraphQLToken)

	resp, err := bkgql.GetPipeline(ctx, client, orgPipelineSlug)
	if err != nil {
		return fmt.Errorf("couldn't retrieve pipeline: %w", err)
	}

	if resp.Pipeline.Id == "" {
		return fmt.Errorf(
			"%w: organization-slug: %s, pipeline-slug: %s",
			ErrNotFound,
			cfg.OrganizationSlug,
			cfg.PipelineSlug,
		)
	}

	debugL.Debug("Pipeline retrieved successfully: %#v", resp)
	l.Info("Signing pipeline with the repository URL:\n%s", resp.Pipeline.Repository.Url)

	pipelineYaml := strings.NewReader(resp.Pipeline.Steps.Yaml)
	parsedPipeline, err := pipeline.Parse(pipelineYaml)
	if err != nil {
		return fmt.Errorf("pipeline parsing failed: %v", err)
	}

	if cfg.Debug {
		enc := yaml.NewEncoder(c.App.Writer)
		enc.SetIndent(yamlIndent)
		if err := enc.Encode(parsedPipeline); err != nil {
			return fmt.Errorf("couldn't encode pipeline: %w", err)
		}
		debugL.Debug("Pipeline parsed successfully: %v", parsedPipeline)
	}

	if err := parsedPipeline.Sign(key, resp.Pipeline.Repository.Url); err != nil {
		return fmt.Errorf("couldn't sign pipeline: %w", err)
	}

	if !cfg.Update {
		enc := yaml.NewEncoder(c.App.Writer)
		enc.SetIndent(yamlIndent)
		return enc.Encode(parsedPipeline)
	}

	signedPipelineYamlBuilder := &strings.Builder{}
	enc := yaml.NewEncoder(signedPipelineYamlBuilder)
	enc.SetIndent(yamlIndent)
	if err := enc.Encode(parsedPipeline); err != nil {
		return fmt.Errorf("couldn't encode signed pipeline: %w", err)
	}

	signedPipelineYaml := strings.TrimSpace(signedPipelineYamlBuilder.String())
	l.Info("Replacing pipeline with signed version:\n%s", signedPipelineYaml)

	updatePipeline, err := promptConfirm(
		c, cfg, "\n\x1b[1mAre you sure you want to update the pipeline? This may break your builds!\x1b[0m",
	)
	if err != nil {
		return fmt.Errorf("couldn't read user input: %w", err)
	}

	if !updatePipeline {
		l.Info("Aborting without updating pipeline")
		return nil
	}

	_, err = bkgql.UpdatePipeline(ctx, client, resp.Pipeline.Id, signedPipelineYaml)
	if err != nil {
		return err
	}

	l.Info("Pipeline updated successfully")

	return nil
}

func promptConfirm(c *cli.Context, cfg *ToolSignConfig, message string) (bool, error) {
	if cfg.NoConfirm {
		return true, nil
	}

	if _, err := fmt.Fprintf(c.App.Writer, "%s [y/N]: ", message); err != nil {
		return false, err
	}

	var input string
	if _, err := fmt.Fscanln(os.Stdin, &input); err != nil {
		return false, err
	}
	input = strings.ToLower(input)

	switch input {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}
