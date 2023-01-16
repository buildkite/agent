package clicommand_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/clicommand"
	"github.com/buildkite/agent/v3/cliconfig"
	"github.com/buildkite/agent/v3/logger"
	"github.com/imdario/mergo"
	"github.com/stretchr/testify/assert"
	"github.com/urfave/cli"
)

func TestPipelineUploadCommand(t *testing.T) {
	// Unset any buildkite env variables so that we don't pick them up in the tests
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "BUILDKITE") {
			key, _, ok := strings.Cut(e, "=")
			if !ok {
				t.Fatalf("This was anticipated to to be impossible so the code is buggy")
			}
			if err := os.Unsetenv(key); err != nil {
				t.Errorf("Error in test: %e", err)
			}
		}
	}

	ctx := context.Background()
	tests := []Test[any]{
		{
			name: "pipeline_upload",
			env: map[string]string{
				"BUILDKITE_AGENT_ACCESS_TOKEN": "llamas",
			},
			args: []string{"buildkite-agent", "pipeline", "upload"},
			expectedConfig: defaultPipelineUploadConfig(&clicommand.PipelineUploadConfig{
				AgentAccessToken: "llamas",
			}),
		},
		{
			name: "pipeline_upload_with_replace",
			env: map[string]string{
				"BUILDKITE_AGENT_ACCESS_TOKEN": "llamas",
			},
			args: []string{"buildkite-agent", "pipeline", "upload", "--replace"},
			expectedConfig: defaultPipelineUploadConfig(&clicommand.PipelineUploadConfig{
				AgentAccessToken: "llamas",
				Replace:          true,
			}),
		},
		{
			name: "pipeline_upload_with_file",
			env: map[string]string{
				"BUILDKITE_AGENT_ACCESS_TOKEN": "llamas",
			},
			args: []string{"buildkite-agent", "pipeline", "upload", "pipeline.yaml"},
			expectedConfig: defaultPipelineUploadConfig(&clicommand.PipelineUploadConfig{
				AgentAccessToken: "llamas",
				FilePath:         "pipeline.yaml",
			}),
		},
		{
			name: "pipeline_upload_with_file_and_replace",
			env: map[string]string{
				"BUILDKITE_AGENT_ACCESS_TOKEN": "llamas",
			},
			args: []string{"buildkite-agent", "pipeline", "upload", "--replace", "pipeline.yaml"},
			expectedConfig: defaultPipelineUploadConfig(&clicommand.PipelineUploadConfig{
				AgentAccessToken: "llamas",
				FilePath:         "pipeline.yaml",
				Replace:          true,
			}),
		},
		{
			name: "pipeline_upload_with_file_and_replace_as_last_arg",
			env: map[string]string{
				"BUILDKITE_AGENT_ACCESS_TOKEN": "llamas",
			},
			args: []string{"buildkite-agent", "pipeline", "upload", "pipeline.yaml", "--replace"},
			expectedConfig: defaultPipelineUploadConfig(&clicommand.PipelineUploadConfig{
				AgentAccessToken: "llamas",
				FilePath:         "pipeline.yaml",
				Replace:          true,
			}),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test := test
			app := cli.NewApp()
			app.Name = "buildkite-agent"
			app.Version = "1"
			app.Action = func(c *cli.Context) {
				t.Errorf("Fell back to default action: %v", c.Args())
			}
			app.CommandNotFound = func(c *cli.Context, command string) {
				t.Errorf("Command not found: %s %v", command, c.Args())
			}
			app.ErrWriter = os.Stderr

			for k, v := range test.env {
				t.Setenv(k, v)
			}

			testAction := &clicommand.AgentAction[clicommand.PipelineUploadConfig]{
				Action: func(
					ctx context.Context,
					c *cli.Context,
					l logger.Logger,
					loader cliconfig.Loader,
					cfg *clicommand.PipelineUploadConfig,
				) error {
					assert.Equal(t, test.expectedConfig, *cfg)
					return nil
				},
			}

			app.Commands = []cli.Command{
				{
					Name:  "pipeline",
					Usage: "Make changes to the pipeline of the currently running build",
					Subcommands: []cli.Command{
						clicommand.PipelineUploadCommand(ctx, testAction),
					},
				},
			}

			if err := app.Run(test.args); err != nil {
				t.Errorf("Error: %v", err)
			}
		})
	}
}

func defaultPipelineUploadConfig(
	cfg *clicommand.PipelineUploadConfig,
) clicommand.PipelineUploadConfig {
	defaultCfg := clicommand.PipelineUploadConfig{
		RedactedVars: []string{
			"*_PASSWORD",
			"*_SECRET",
			"*_TOKEN",
			"*_ACCESS_KEY",
			"*_SECRET_KEY",
		},
		LogLevel:    "notice",
		Experiments: []string{},
		Endpoint:    "https://agent.buildkite.com/v3",
	}

	if cfg != nil {
		_ = mergo.MergeWithOverwrite(&defaultCfg, cfg)
	}

	return defaultCfg
}
