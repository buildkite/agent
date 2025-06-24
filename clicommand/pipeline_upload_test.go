package clicommand

import (
	"context"
	"runtime"
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/internal/experiments"
	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/go-pipeline"
	"github.com/buildkite/go-pipeline/ordered"
	"github.com/google/go-cmp/cmp"
)

func TestSearchForSecrets(t *testing.T) {
	t.Parallel()

	cfg := &PipelineUploadConfig{
		RedactedVars:         []string{"SEKRET", "SSH_KEY"},
		RejectSecrets:        true,
		ValidateDependencies: false,
	}

	plainPipeline := &pipeline.Pipeline{
		Steps: pipeline.Steps{
			&pipeline.CommandStep{
				Command: "secret squirrels and alpacas",
			},
		},
	}

	tests := []struct {
		desc     string
		environ  map[string]string
		pipeline *pipeline.Pipeline
		wantLog  string
	}{
		{
			desc:     "no secret",
			environ:  map[string]string{"SEKRET": "llamas", "UNRELATED": "horses"},
			pipeline: plainPipeline,
			wantLog:  "",
		},
		{
			desc:     "one secret",
			environ:  map[string]string{"SEKRET": "squirrel", "PYTHON": "not a chance"},
			pipeline: plainPipeline,
			wantLog:  `pipeline "cat-o-matic.yaml" contains values interpolated from the following secret environment variables: [SEKRET], and cannot be uploaded to Buildkite`,
		},
		{
			desc:     "two secrets",
			environ:  map[string]string{"SEKRET": "squirrel", "SSH_KEY": "alpacas", "SPECIES": "Felix sylvestris"},
			pipeline: plainPipeline,
			wantLog:  `pipeline "cat-o-matic.yaml" contains values interpolated from the following secret environment variables: [SEKRET SSH_KEY], and cannot be uploaded to Buildkite`,
		},
		{
			desc:    "one step env secret",
			environ: nil,
			pipeline: &pipeline.Pipeline{
				Steps: pipeline.Steps{
					&pipeline.CommandStep{
						Command: "secret llamas and alpacas",
						Env:     map[string]string{"SEKRET": "squirrels", "UNRELATED": "horses"},
					},
				},
			},
			wantLog: `pipeline "cat-o-matic.yaml" contains values interpolated from the following secret environment variables: [SEKRET], and cannot be uploaded to Buildkite`,
		},
		{
			desc:    "one step env secret within a group",
			environ: nil,
			pipeline: &pipeline.Pipeline{
				Steps: pipeline.Steps{
					&pipeline.GroupStep{
						Steps: pipeline.Steps{
							&pipeline.CommandStep{
								Command: "secret llamas and alpacas",
								Env:     map[string]string{"SEKRET": "squirrels", "UNRELATED": "horses"},
							},
						},
					},
				},
			},
			wantLog: `pipeline "cat-o-matic.yaml" contains values interpolated from the following secret environment variables: [SEKRET], and cannot be uploaded to Buildkite`,
		},
		{
			desc:    "one pipeline env secret",
			environ: nil,
			pipeline: &pipeline.Pipeline{
				Env: ordered.MapFromItems(
					ordered.TupleSS{Key: "SEKRET", Value: "squirrel"},
					ordered.TupleSS{Key: "UNRELATED", Value: "horses"},
				),
				Steps: pipeline.Steps{
					&pipeline.CommandStep{
						Command: "secret llamas and alpacas",
					},
				},
			},
			wantLog: `pipeline "cat-o-matic.yaml" contains values interpolated from the following secret environment variables: [SEKRET], and cannot be uploaded to Buildkite`,
		},
		{
			desc:    "step env 'secret' that is actually runtime env interpolation",
			environ: nil,
			pipeline: &pipeline.Pipeline{
				Steps: pipeline.Steps{
					&pipeline.CommandStep{
						Command: "secret llamas and alpacas",
						Env:     map[string]string{"SEKRET": "$SQUIRREL", "UNRELATED": "horses"},
					},
				},
			},
			wantLog: "",
		},
		{
			desc:    "pipeline env 'secret' that is actually runtime env interpolation",
			environ: nil,
			pipeline: &pipeline.Pipeline{
				Env: ordered.MapFromItems(
					ordered.TupleSS{Key: "SEKRET", Value: "${SQUIRREL}"},
					ordered.TupleSS{Key: "UNRELATED", Value: "horses"},
				),
				Steps: pipeline.Steps{
					&pipeline.CommandStep{
						Command: "secret llamas and alpacas",
					},
				},
			},
			wantLog: "",
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()
			l := logger.NewBuffer()
			err := searchForSecrets(l, cfg, env.FromMap(test.environ), test.pipeline, "cat-o-matic.yaml")
			if len(test.wantLog) == 0 {
				if err != nil {
					t.Errorf("searchForSecrets(l, %v, %v, %v, %q) = %v", cfg, test.environ, test.pipeline, "cat-o-matic.yaml", err)
				}
				return
			}
			if !strings.Contains(err.Error(), test.wantLog) {
				t.Errorf("searchForSecrets(l, %v, %v, %v, %q) = %v, want error string containing %q",
					cfg, test.environ, test.pipeline, "cat-o-matic.yaml", err, test.wantLog)
			}
		})
	}
}

// Most of this is tested in go-pipeline, here we just need to check that env.Environment
// also works with go-pipeline's interpolation.
func TestPipelineInterpolationCaseSensitivity(t *testing.T) {
	t.Parallel()

	cfg := &PipelineUploadConfig{
		RedactedVars:  []string{},
		RejectSecrets: true,
	}

	// this is the data structure we use for environment variables in the agent
	// we test here it is suitable for interpolation with platform-dependent case sensitivity
	environ := env.FromMap(map[string]string{
		"FOO": "bar",
	})

	const pipelineYAML = `---
steps:
- command: echo $foo
`

	var wantPipelines []*pipeline.Pipeline
	if runtime.GOOS == "windows" {
		wantPipelines = []*pipeline.Pipeline{{
			Steps: pipeline.Steps{
				&pipeline.CommandStep{
					Command: "echo bar",
				},
			},
		}}
	} else {
		wantPipelines = []*pipeline.Pipeline{{
			Steps: pipeline.Steps{
				&pipeline.CommandStep{
					Command: "echo ",
				},
			},
		}}
	}
	ctx := context.Background()

	var gotPipelines []*pipeline.Pipeline

	for p, err := range cfg.parseAndInterpolate(ctx, "test", strings.NewReader(pipelineYAML), environ) {
		if err != nil {
			t.Errorf(`cfg.parseAndInterpolate(ctx, "test", %q, %v) = %v; want nil`, pipelineYAML, environ, err)
		}
		gotPipelines = append(gotPipelines, p)
	}
	if diff := cmp.Diff(gotPipelines, wantPipelines, cmp.Comparer(ordered.EqualSA), cmp.Comparer(ordered.EqualSS)); diff != "" {
		t.Errorf("pipelines diff (-got +want):\n%s", diff)
	}
}

func TestPipelineInterpolationRuntimeEnvPrecedence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		desc             string
		preferRuntimeEnv bool
		wantCommands     []string
	}{
		{
			desc:             "With experiment disabled",
			preferRuntimeEnv: false,
			wantCommands:     []string{"echo Hi bob"},
		},
		{
			desc:             "With experiment enabled",
			preferRuntimeEnv: true,
			wantCommands:     []string{"echo Hi alice"},
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			// With the experiment enabled this variable takes precedence over the one defined in the pipeline yaml
			environ := env.FromMap(map[string]string{
				"NAME": "alice",
			})

			const pipelineYAML = `---
env:
  NAME: bob
  GREETING: "Hi ${NAME:-}"
steps:
- command: echo $GREETING
`
			cfg := &PipelineUploadConfig{
				RedactedVars:  []string{},
				RejectSecrets: true,
			}
			ctx := context.Background()
			if test.preferRuntimeEnv {
				ctx, _ = experiments.Enable(ctx, experiments.InterpolationPrefersRuntimeEnv)
			}

			var gotCommands []string

			for p, err := range cfg.parseAndInterpolate(ctx, "test", strings.NewReader(pipelineYAML), environ) {
				if err != nil {
					t.Errorf(`cfg.parseAndInterpolate(ctx, "test", %q, %v) = %v; want nil`, pipelineYAML, environ, err)
				}
				s := p.Steps[len(p.Steps)-1]
				commandStep, ok := s.(*pipeline.CommandStep)
				if !ok {
					t.Errorf("Invalid pipeline step %v", s)
				}
				gotCommands = append(gotCommands, commandStep.Command)
			}

			if diff := cmp.Diff(gotCommands, test.wantCommands); diff != "" {
				t.Errorf("commands diff (-got +want):\n%s", diff)
			}
		})
	}
}

func TestPipelineInterpolation_Regression3358(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		interpolation bool
		wantCommands  []string
	}{
		{
			name:          "with interpolation",
			interpolation: true,
			wantCommands:  []string{"echo Hi bob"},
		},
		{
			name:          "without interpolation",
			interpolation: false,
			wantCommands:  []string{"echo $GREETING"},
		},
	}

	environ := env.FromMap(map[string]string{
		"NAME": "alice",
	})

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			const pipelineYAML = `---
env:
  NAME: bob
  GREETING: "Hi ${NAME:-}"
steps:
- command: echo $GREETING
`
			cfg := &PipelineUploadConfig{
				NoInterpolation: !test.interpolation,
				RedactedVars:    []string{},
				RejectSecrets:   true,
			}
			ctx := context.Background()

			var gotCommands []string

			for p, err := range cfg.parseAndInterpolate(ctx, "test", strings.NewReader(pipelineYAML), environ) {
				if err != nil {
					t.Errorf(`cfg.parseAndInterpolate(ctx, "test", %q, %v) = %v; want nil`, pipelineYAML, environ, err)
				}
				s := p.Steps[len(p.Steps)-1]
				commandStep, ok := s.(*pipeline.CommandStep)
				if !ok {
					t.Errorf("Invalid pipeline step %v", s)
				}
				gotCommands = append(gotCommands, commandStep.Command)
			}

			if diff := cmp.Diff(gotCommands, test.wantCommands); diff != "" {
				t.Errorf("commands diff (-got +want):\n%s", diff)
			}
		})
	}
}
