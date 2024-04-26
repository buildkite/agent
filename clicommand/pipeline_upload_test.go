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
	"gotest.tools/v3/assert"
)

func TestSearchForSecrets(t *testing.T) {
	t.Parallel()

	cfg := &PipelineUploadConfig{
		RedactedVars:  []string{"SEKRET", "SSH_KEY"},
		RejectSecrets: true,
	}

	p := &pipeline.Pipeline{
		Steps: pipeline.Steps{
			&pipeline.CommandStep{
				Command: "secret squirrels and alpacas",
			},
		},
	}

	tests := []struct {
		desc    string
		environ map[string]string
		wantLog string
	}{
		{
			desc:    "no secret",
			environ: map[string]string{"SEKRET": "llamas", "UNRELATED": "horses"},
			wantLog: "",
		},
		{
			desc:    "one secret",
			environ: map[string]string{"SEKRET": "squirrel", "PYTHON": "not a chance"},
			wantLog: `pipeline "cat-o-matic.yaml" contains values interpolated from the following secret environment variables: [SEKRET], and cannot be uploaded to Buildkite`,
		},
		{
			desc:    "two secrets",
			environ: map[string]string{"SEKRET": "squirrel", "SSH_KEY": "alpacas", "SPECIES": "Felix sylvestris"},
			wantLog: `pipeline "cat-o-matic.yaml" contains values interpolated from the following secret environment variables: [SEKRET SSH_KEY], and cannot be uploaded to Buildkite`,
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()
			l := logger.NewBuffer()
			err := searchForSecrets(l, cfg, test.environ, p, "cat-o-matic.yaml")
			if len(test.wantLog) == 0 {
				assert.NilError(t, err)
				return
			}
			assert.ErrorContains(t, err, test.wantLog)
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

	var expectedPipeline *pipeline.Pipeline
	if runtime.GOOS == "windows" {
		expectedPipeline = &pipeline.Pipeline{
			Steps: pipeline.Steps{
				&pipeline.CommandStep{
					Command: "echo bar",
				},
			},
		}
	} else {
		expectedPipeline = &pipeline.Pipeline{
			Steps: pipeline.Steps{
				&pipeline.CommandStep{
					Command: "echo ",
				},
			},
		}
	}
	ctx := context.Background()

	p, err := cfg.parseAndInterpolate("test", strings.NewReader(pipelineYAML), environ, ctx)
	assert.NilError(t, err, `cfg.parseAndInterpolate("test", %q, %q) = %v; want nil`, pipelineYAML, environ, err)
	assert.DeepEqual(t, p, expectedPipeline, cmp.Comparer(ordered.EqualSA), cmp.Comparer(ordered.EqualSS))
}

func TestPipelineInterpolationRuntimeEnvPrecedence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		desc             string
		preferRuntimeEnv bool
		expectedCommand  string
	}{
		{
			desc:             "With experiment disabled",
			preferRuntimeEnv: false,
			expectedCommand:  "echo Hi bob",
		},
		{
			desc:             "With experiment enabled",
			preferRuntimeEnv: true,
			expectedCommand:  "echo Hi alice",
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

			p, err := cfg.parseAndInterpolate("test", strings.NewReader(pipelineYAML), environ, ctx)
			assert.NilError(t, err, `cfg.parseAndInterpolate("test", %q, %q) = %v; want nil`, pipelineYAML, environ, err)
			s := p.Steps[len(p.Steps)-1]
			commandStep, ok := s.(*pipeline.CommandStep)
			if !ok {
				t.Errorf("Invalid pipeline step %v", s)
			}
			assert.Equal(t, commandStep.Command, test.expectedCommand)
		})
	}
}
