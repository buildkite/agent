package clicommand

import (
	"testing"

	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/go-pipeline"
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
		test := test
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
