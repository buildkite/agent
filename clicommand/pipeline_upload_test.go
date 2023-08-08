package clicommand

import (
	"testing"

	"github.com/buildkite/agent/v3/internal/pipeline"
	"github.com/buildkite/agent/v3/logger"
	"github.com/google/go-cmp/cmp"
)

func TestSearchForSecrets(t *testing.T) {
	t.Parallel()

	cfg := &PipelineUploadConfig{
		RedactedVars:  []string{"SEKRET", "SSH_KEY"},
		RejectSecrets: true,
	}

	pipeline := &pipeline.Pipeline{
		Steps: pipeline.Steps{
			&pipeline.CommandStep{
				Command: "secret squirrels and alpacas",
			},
		},
	}

	tests := []struct {
		desc    string
		environ map[string]string
		wantLog []string
	}{
		{
			desc:    "no secret",
			environ: map[string]string{"SEKRET": "llamas", "UNRELATED": "horses"},
			wantLog: []string{},
		},
		{
			desc:    "one secret",
			environ: map[string]string{"SEKRET": "squirrel", "PYTHON": "not a chance"},
			wantLog: []string{
				`[fatal] Pipeline "cat-o-matic.yaml" contains values interpolated from the following secret environment variables: [SEKRET], and cannot be uploaded to Buildkite`,
			},
		},
		{
			desc:    "two secrets",
			environ: map[string]string{"SEKRET": "squirrel", "SSH_KEY": "alpacas", "SPECIES": "Felix sylvestris"},
			wantLog: []string{
				`[fatal] Pipeline "cat-o-matic.yaml" contains values interpolated from the following secret environment variables: [SEKRET SSH_KEY], and cannot be uploaded to Buildkite`,
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()
			l := logger.NewBuffer()

			searchForSecrets(l, cfg, test.environ, pipeline, "cat-o-matic.yaml")

			if diff := cmp.Diff(l.Messages, test.wantLog); diff != "" {
				t.Errorf("searchForSecrets log output diff (-got +want):\n%s", diff)
			}
		})
	}
}
