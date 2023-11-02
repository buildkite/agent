package clicommand

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Khan/genqlient/graphql"
	"github.com/buildkite/agent/v3/internal/bkgql"
	"github.com/buildkite/agent/v3/internal/pipeline"
	"github.com/buildkite/agent/v3/internal/stdin"
	"github.com/buildkite/agent/v3/logger"
	"github.com/urfave/cli"
	"gopkg.in/yaml.v3"
)

type ToolSignConfig struct {
	FilePath string `cli:"arg:0" label:"upload paths"`

	UpdateOnline bool `cli:"update-online"`

	JWKSFilePath string `cli:"jwks-file-path"`
	SigningKeyID string `cli:"signing-key-id"`

	// Pipeline invariants
	OrganizationSlug string `cli:"organization-slug"`
	OrganizationUUID string `cli:"organization-uuid"`
	PipelineSlug     string `cli:"pipeline-slug"`
	PipelineUUID     string `cli:"pipeline-uuid"`
	Repository       string `cli:"repo"`

	GraphQLToken string `cli:"graphql-token"`

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
	ErrNeedsGraphQLToken = errors.New("can't update pipeline online without a GraphQL token")
	ErrNoGraphQLID       = errors.New("invalid pipeline GraphQL ID")
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
		cli.BoolFlag{
			Name:   "update-online",
			Usage:  "Update the pipeline online after signing it. This can only be used if the GraphQL token is provided.",
			EnvVar: "BUILDKITE_TOOL_SIGN_UPDATE_ONLINE",
		},

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

		cli.StringFlag{
			Name:     "graphql-token",
			Usage:    "A token for the buildkite graphql API. This will be used to populate the pipeline invariants if they are not provided.",
			EnvVar:   "BUILDKITE_GRAPHQL_TOKEN",
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

		var (
			parserInput *pipelineParserInput
			client      graphql.Client
			err         error
		)
		if cfg.GraphQLToken == "" {
			parserInput, err = parserInputsFromArgsOrStdin(l, &cfg)
			if err != nil {
				return err
			}
		} else {
			client = bkgql.NewClient(cfg.GraphQLToken)
			parserInput, err = parserInputsFromGraphQL(ctx, l, client, &cfg)
			if err != nil {
				return err
			}
		}

		// Parse the pipeline
		parsedPipeline, err := pipeline.Parse(parserInput.reader)
		if err != nil {
			return fmt.Errorf("pipeline parsing of %q failed: %v", parserInput.source, err)
		}

		l.Debug("Pipeline parsed successfully: %v", parsedPipeline)

		key, err := loadSigningKey(&cfg)
		if err != nil {
			return fmt.Errorf("couldn't read the signing key file: %w", err)
		}

		if err := parsedPipeline.Sign(key, &parserInput.pipelineInvariants); err != nil {
			return fmt.Errorf("couldn't sign pipeline: %w", err)
		}

		if !cfg.UpdateOnline {
			enc := yaml.NewEncoder(c.App.Writer)
			enc.SetIndent(yamlIndent)
			return enc.Encode(parsedPipeline)
		}

		if client == nil {
			return ErrNeedsGraphQLToken
		}

		if parserInput.pipelineGraphQLID == "" {
			return ErrUseGraphQL
		}

		l.Info("Updating pipeline online")

		signedPipelineYAML := &strings.Builder{}
		enc := yaml.NewEncoder(signedPipelineYAML)
		enc.SetIndent(yamlIndent)
		if err := enc.Encode(parsedPipeline); err != nil {
			return fmt.Errorf("couldn't encode signed pipeline: %w", err)
		}

		if _, err := bkgql.UpdatePipeline(
			ctx,
			client,
			parserInput.pipelineGraphQLID,
			signedPipelineYAML.String(),
		); err != nil {
			return fmt.Errorf("couldn't update pipeline online: %w", err)
		}

		return nil
	},
}

func (cfg *ToolSignConfig) jwksFilePath() string {
	return cfg.JWKSFilePath
}

func (cfg *ToolSignConfig) signingKeyId() string {
	return cfg.SigningKeyID
}

type pipelineParserInput struct {
	reader             io.Reader
	source             string
	pipelineGraphQLID  string
	pipelineInvariants pipeline.PipelineInvariants
}

func parserInputsFromArgsOrStdin(
	l logger.Logger,
	cfg *ToolSignConfig,
) (*pipelineParserInput, error) {
	if cfg.OrganizationUUID == "" || cfg.PipelineUUID == "" || cfg.Repository == "" {
		return nil, ErrUseGraphQL
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
			return nil, fmt.Errorf("failed to read file: %w", err)
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
		return nil, ErrNoPipeline
	}

	return &pipelineParserInput{
		reader: input,
		source: filename,
		pipelineInvariants: pipeline.PipelineInvariants{
			OrganizationUUID: cfg.OrganizationUUID,
			OrganizationSlug: cfg.OrganizationSlug,
			PipelineUUID:     cfg.PipelineUUID,
			PipelineSlug:     cfg.PipelineSlug,
			Repository:       cfg.Repository,
		},
	}, nil
}

func parserInputsFromGraphQL(
	ctx context.Context,
	l logger.Logger,
	client graphql.Client,
	cfg *ToolSignConfig,
) (*pipelineParserInput, error) {
	l.Info("Retrieving pipeline from the GraphQL API")

	orgPipelineSlug := fmt.Sprintf("%s/%s", cfg.OrganizationSlug, cfg.PipelineSlug)
	l = l.WithFields(logger.StringField("orgPipelineSlug", orgPipelineSlug))

	resp, err := bkgql.GetPipeline(ctx, client, orgPipelineSlug)
	if err != nil {
		return nil, fmt.Errorf("couldn't retrieve pipeline: %w", err)
	}

	l.Debug("Pipeline retrieved successfully: %#v", resp)

	return &pipelineParserInput{
		reader:            strings.NewReader(resp.Pipeline.Steps.Yaml),
		source:            "(graphql)",
		pipelineGraphQLID: resp.Pipeline.Id,
		pipelineInvariants: pipeline.PipelineInvariants{
			OrganizationUUID: resp.Pipeline.Organization.Uuid,
			OrganizationSlug: cfg.PipelineSlug,
			PipelineUUID:     resp.Pipeline.Uuid,
			PipelineSlug:     cfg.PipelineSlug,
			Repository:       resp.Pipeline.Repository.Url,
		},
	}, nil
}
