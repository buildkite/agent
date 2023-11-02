package clicommand

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/buildkite/agent/v3/internal/bkgql"
	"github.com/buildkite/agent/v3/internal/pipeline"
	"github.com/buildkite/agent/v3/internal/stdin"
	"github.com/buildkite/agent/v3/logger"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/urfave/cli"
	"gopkg.in/yaml.v3"
)

type ToolSignConfig struct {
	FilePath string `cli:"arg:0" label:"upload paths"`

	// These are required to do anything
	JWKSFilePath string `cli:"jwks-file-path"`
	SigningKeyID string `cli:"signing-key-id"`

	// These change the behaviour
	GraphQLToken string `cli:"graphql-token"`
	UpdateOnline bool   `cli:"update-online"`

	// Pipeline invariants
	OrganizationSlug string `cli:"organization-slug"`
	OrganizationUUID string `cli:"organization-uuid"`
	PipelineSlug     string `cli:"pipeline-slug"`
	PipelineUUID     string `cli:"pipeline-uuid"`
	Repository       string `cli:"repo"`

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
		"either provide the pipeline YAML, organization UUID, pipeline UUID, and the repository URL, " +
			"or provide a GraphQL token to allow them to be retrieved",
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
			Name:   "update-online",
			Usage:  "Update the pipeline online after signing it. This can only be used if the GraphQL token is provided.",
			EnvVar: "BUILDKITE_TOOL_SIGN_UPDATE_ONLINE",
		},

		// These are required to do anything
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
			Name:     "organization-uuid",
			Usage:    "The UUID of the organization, which is used in the pipeline signature. If the GraphQL token is provided, this will be ignored.",
			EnvVar:   "BUILDKITE_ORGANIZATION_UUID",
			Required: false,
		},
		cli.StringFlag{
			Name:     "pipeline-slug",
			Usage:    "The pipeline slug to use when signing the pipeline.",
			EnvVar:   "BUILDKITE_PIPELINE_SLUG",
			Required: true,
		},
		cli.StringFlag{
			Name:     "pipeline-uuid",
			Usage:    "The UUID of the pipeline, which is used in the pipeline signature. If the GraphQL token is provided, this will be ignored.",
			EnvVar:   "BUILDKITE_PIPELINE_UUID",
			Required: false,
		},
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

		key, err := loadSigningKey(&cfg)
		if err != nil {
			return fmt.Errorf("couldn't read the signing key file: %w", err)
		}

		if cfg.GraphQLToken == "" {
			return signOffline(c, l, key, &cfg)
		}

		return signWithGraphQL(ctx, c, l, key, &cfg)
	},
}

func (cfg *ToolSignConfig) jwksFilePath() string {
	return cfg.JWKSFilePath
}

func (cfg *ToolSignConfig) signingKeyId() string {
	return cfg.SigningKeyID
}

func signOffline(
	c *cli.Context,
	l logger.Logger,
	key jwk.Key,
	cfg *ToolSignConfig,
) error {
	if cfg.OrganizationUUID == "" || cfg.PipelineUUID == "" || cfg.Repository == "" {
		return ErrUseGraphQL
	}

	pipelineInvariants := pipeline.PipelineInvariants{
		OrganizationUUID: cfg.OrganizationUUID,
		OrganizationSlug: cfg.OrganizationSlug,
		PipelineUUID:     cfg.PipelineUUID,
		PipelineSlug:     cfg.PipelineSlug,
		Repository:       cfg.Repository,
	}

	// Find the pipeline either from STDIN or the first argument
	var (
		input    io.Reader
		filename string
	)

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

		input = os.Stdin
		filename = "(stdin)"

	default:
		return ErrNoPipeline
	}

	parsedPipeline, err := pipeline.Parse(input)
	if err != nil {
		return fmt.Errorf("pipeline parsing of %q failed: %v", filename, err)
	}

	l.Debug("Pipeline parsed successfully: %v", parsedPipeline)

	if err := parsedPipeline.Sign(key, &pipelineInvariants); err != nil {
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
	l = l.WithFields(logger.StringField("orgPipelineSlug", orgPipelineSlug))

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

	l.Debug("Pipeline retrieved successfully: %#v", resp)

	pipelineInvariants := pipeline.PipelineInvariants{
		OrganizationUUID: resp.Pipeline.Organization.Uuid,
		OrganizationSlug: cfg.OrganizationSlug,
		PipelineUUID:     resp.Pipeline.Uuid,
		PipelineSlug:     cfg.PipelineSlug,
		Repository:       resp.Pipeline.Repository.Url,
	}

	pipelineYaml := strings.NewReader(resp.Pipeline.Steps.Yaml)
	parsedPipeline, err := pipeline.Parse(pipelineYaml)
	if err != nil {
		return fmt.Errorf("pipeline parsing failed: %v", err)
	}

	l.Debug("Pipeline parsed successfully: %v", parsedPipeline)

	if err := parsedPipeline.Sign(key, &pipelineInvariants); err != nil {
		return fmt.Errorf("couldn't sign pipeline: %w", err)
	}

	if !cfg.UpdateOnline {
		enc := yaml.NewEncoder(c.App.Writer)
		enc.SetIndent(yamlIndent)
		return enc.Encode(parsedPipeline)
	}

	l.Info("Updating pipeline online")

	signedPipelineYAML := &strings.Builder{}
	enc := yaml.NewEncoder(signedPipelineYAML)
	enc.SetIndent(yamlIndent)
	if err := enc.Encode(parsedPipeline); err != nil {
		return fmt.Errorf("couldn't encode signed pipeline: %w", err)
	}

	_, err = bkgql.UpdatePipeline(ctx, client, resp.Pipeline.Id, signedPipelineYAML.String())
	return err
}
