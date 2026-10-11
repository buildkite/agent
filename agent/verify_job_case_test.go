package agent

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/logger"
	"github.com/buildkite/go-pipeline"
	"github.com/buildkite/go-pipeline/signature"
	"github.com/lestrrat-go/jwx/v3/jwk"
)

func TestVerifyJobPreservesSignedMixedCaseEnvNames(t *testing.T) {
	t.Parallel()

	key, err := jwk.Import([]byte("synthetic-signing-key-with-32-bytes-or-more"))
	if err != nil {
		t.Fatal(err)
	}
	if err := key.Set("alg", "HS256"); err != nil {
		t.Fatal(err)
	}
	if err := key.Set("kid", "test-key"); err != nil {
		t.Fatal(err)
	}
	keySet := jwk.NewSet()
	if err := keySet.AddKey(key); err != nil {
		t.Fatal(err)
	}

	const repo = "https://example.test/repo.git"
	step := pipeline.CommandStep{
		Command: "echo hello",
		Env: map[string]string{
			"StepMixedCase": "step-value",
		},
	}
	if err := signature.SignSteps(t.Context(), pipeline.Steps{&step}, key, repo,
		signature.WithEnv(map[string]string{"PipelineMixedCase": "pipeline-value"})); err != nil {
		t.Fatal(err)
	}

	original := map[string]string{
		"BUILDKITE_REPO":    repo,
		"BUILDKITE_COMMAND": step.Command,
		"StepMixedCase":     "step-value",
		"PipelineMixedCase": "pipeline-value",
	}
	normalized, err := normalizeJobEnv(original, true)
	if err != nil {
		t.Fatal(err)
	}
	runner := &JobRunner{
		agentLogger:    logger.Discard,
		originalJobEnv: original,
		conf: JobRunnerConfig{Job: &api.Job{
			Step: step,
			Env:  normalized,
		}},
	}
	if err := runner.verifyJob(t.Context(), keySet); err != nil {
		t.Fatalf("signed mixed-case env rejected after normalization: %v", err)
	}

	// Demonstrate why signature verification needs the exact signed spelling.
	runner.originalJobEnv = nil
	if err := runner.verifyJob(t.Context(), keySet); err == nil {
		t.Fatal("normalized env unexpectedly verified the mixed-case signature")
	}
}

func TestVerifyJobComparesWindowsPluginAliasWithSignedStep(t *testing.T) {
	t.Parallel()

	key, err := jwk.Import([]byte("synthetic-signing-key-with-32-bytes-or-more"))
	if err != nil {
		t.Fatal(err)
	}
	if err := key.Set("alg", "HS256"); err != nil {
		t.Fatal(err)
	}
	if err := key.Set("kid", "test-key"); err != nil {
		t.Fatal(err)
	}
	keySet := jwk.NewSet()
	if err := keySet.AddKey(key); err != nil {
		t.Fatal(err)
	}

	const repo = "https://example.test/repo.git"
	plugins := pipeline.Plugins{{Source: "github.com/buildkite-plugins/example#v1"}}
	pluginsJSON, err := json.Marshal(plugins)
	if err != nil {
		t.Fatal(err)
	}

	for _, tt := range []struct {
		name        string
		signed      pipeline.Plugins
		jobPlugins  string
		wantInvalid bool
	}{
		{name: "unsigned plugin", jobPlugins: string(pluginsJSON), wantInvalid: true},
		{name: "signed plugin", signed: plugins, jobPlugins: string(pluginsJSON)},
		{name: "no plugins", jobPlugins: "[]"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			step := pipeline.CommandStep{Command: "echo hello", Plugins: tt.signed}
			if err := signature.SignSteps(t.Context(), pipeline.Steps{&step}, key, repo); err != nil {
				t.Fatal(err)
			}

			original := map[string]string{
				"BUILDKITE_REPO":    repo,
				"BUILDKITE_COMMAND": step.Command,
				"buildkite_plugins": tt.jobPlugins,
			}
			normalized, err := normalizeJobEnv(original, true)
			if err != nil {
				t.Fatal(err)
			}
			runner := &JobRunner{
				agentLogger:    logger.Discard,
				originalJobEnv: original,
				conf: JobRunnerConfig{Job: &api.Job{
					Step: step,
					Env:  normalized,
				}},
			}
			err = runner.verifyJob(t.Context(), keySet)
			if tt.wantInvalid {
				if !errors.Is(err, ErrInvalidJob) {
					t.Fatalf("verifyJob() error = %v, want ErrInvalidJob", err)
				}
			} else if err != nil {
				t.Fatalf("verifyJob() error = %v, want nil", err)
			}
		})
	}
}
