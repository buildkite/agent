package clicommand

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/buildkite/go-pipeline"
	"github.com/buildkite/go-pipeline/jwkutil"
	"github.com/buildkite/go-pipeline/signature"
	"github.com/google/go-cmp/cmp"
	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/urfave/cli/v3"
)

// TestMergePipelineSecretsIntoSteps covers the merge behaviour shared by
// signOffline, signWithGraphQL and pipeline_upload.go: pipeline-level
// secrets should reach every command step, including those nested inside
// group steps, without clobbering a step's own secrets.
func TestMergePipelineSecretsIntoSteps(t *testing.T) {
	t.Parallel()

	pipelineSecrets := pipeline.Secrets{
		{Key: "db-password", EnvironmentVariable: "DB_PASSWORD"},
	}

	steps := pipeline.Steps{
		&pipeline.CommandStep{
			Label:   "top-level, no own secrets",
			Command: "echo top",
		},
		&pipeline.CommandStep{
			Label:   "top-level, own secret for the same env var",
			Command: "echo override",
			Secrets: pipeline.Secrets{
				{Key: "other-password", EnvironmentVariable: "DB_PASSWORD"},
			},
		},
		&pipeline.GroupStep{
			Steps: pipeline.Steps{
				&pipeline.CommandStep{
					Label:   "nested in a group, no own secrets",
					Command: "echo nested",
				},
			},
		},
	}

	mergePipelineSecretsIntoSteps(steps, pipelineSecrets)

	top := steps[0].(*pipeline.CommandStep)
	if diff := cmp.Diff(top.Secrets, pipelineSecrets); diff != "" {
		t.Errorf("top-level step secrets (-got +want):\n%s", diff)
	}

	overridden := steps[1].(*pipeline.CommandStep)
	wantOverridden := pipeline.Secrets{{Key: "other-password", EnvironmentVariable: "DB_PASSWORD"}}
	if diff := cmp.Diff(overridden.Secrets, wantOverridden); diff != "" {
		t.Errorf("step-level secret should take precedence over a pipeline-level secret for the same environment variable (-got +want):\n%s", diff)
	}

	group := steps[2].(*pipeline.GroupStep)
	nested := group.Steps[0].(*pipeline.CommandStep)
	if diff := cmp.Diff(nested.Secrets, pipelineSecrets); diff != "" {
		t.Errorf("command step nested inside a group step secrets (-got +want):\n%s", diff)
	}
}

// TestSignOfflineMergesPipelineLevelSecrets is a regression test for pipeline-level
// secrets not reaching command steps before signing, particularly ones nested inside
// group steps. It drives the real `tool sign` CLI command end to end: signing a
// pipeline whose secrets are declared only at the pipeline level, then verifying the
// resulting signatures.
func TestSignOfflineMergesPipelineLevelSecrets(t *testing.T) {
	t.Parallel()

	privSet, pubSet, err := jwkutil.NewKeyPair("test-key", jwa.EdDSA())
	if err != nil {
		t.Fatalf("jwkutil.NewKeyPair() error = %v", err)
	}

	jwksBytes, err := json.Marshal(privSet)
	if err != nil {
		t.Fatalf("marshaling JWKS: %v", err)
	}
	jwksPath := filepath.Join(t.TempDir(), "signing-jwks.json")
	if err := os.WriteFile(jwksPath, jwksBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	const pipelineYAML = `---
secrets:
  - key: db-password
    environment_variable: DB_PASSWORD
steps:
  - label: top-level
    command: echo top
  - group: "grouped"
    steps:
      - label: nested
        command: echo nested
`
	pipelinePath := filepath.Join(t.TempDir(), "pipeline.yml")
	if err := os.WriteFile(pipelinePath, []byte(pipelineYAML), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	cmd := *ToolSignCommand
	app := &cli.Command{Name: "buildkite-agent", Commands: []*cli.Command{&cmd}, Writer: &stdout, ErrWriter: io.Discard}
	if err := app.Run(t.Context(), []string{
		"buildkite-agent", "sign",
		"--jwks-file", jwksPath,
		"--repo", "fake-repo",
		pipelinePath,
	}); err != nil {
		t.Fatalf("tool sign error = %v, want success", err)
	}

	signedPipeline, err := pipeline.Parse(strings.NewReader(stdout.String()))
	if err != nil {
		t.Fatalf("parsing signed pipeline output: %v\noutput:\n%s", err, stdout.String())
	}

	top, ok := signedPipeline.Steps[0].(*pipeline.CommandStep)
	if !ok {
		t.Fatalf("Steps[0] = %T, want *pipeline.CommandStep", signedPipeline.Steps[0])
	}
	group, ok := signedPipeline.Steps[1].(*pipeline.GroupStep)
	if !ok {
		t.Fatalf("Steps[1] = %T, want *pipeline.GroupStep", signedPipeline.Steps[1])
	}
	nested, ok := group.Steps[0].(*pipeline.CommandStep)
	if !ok {
		t.Fatalf("group.Steps[0] = %T, want *pipeline.CommandStep", group.Steps[0])
	}

	wantSecrets := pipeline.Secrets{{Key: "db-password", EnvironmentVariable: "DB_PASSWORD"}}
	for _, step := range []*pipeline.CommandStep{top, nested} {
		if diff := cmp.Diff(step.Secrets, wantSecrets); diff != "" {
			t.Errorf("step %q secrets (-got +want):\n%s", step.Label, diff)
		}
		if step.Signature == nil {
			t.Fatalf("step %q has no signature", step.Label)
		}
		if !slices.Contains(step.Signature.SignedFields, "secrets") {
			t.Errorf("step %q signed_fields = %v, want it to contain %q", step.Label, step.Signature.SignedFields, "secrets")
		}

		if err := signature.VerifyStep(t.Context(), step, pubSet, "fake-repo", signature.WithEnv(signedPipeline.Env.ToMap())); err != nil {
			t.Errorf("signature.VerifyStep(%q) error = %v, want success", step.Label, err)
		}
	}
}
