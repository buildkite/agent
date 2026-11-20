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

// TestMergeSecretsStepFirst is a regression test for pipeline.Secrets.MergeWith
// (used by pipeline.CommandStep.MergeSecretsFromPipeline): it returns one side
// verbatim, skipping deduplication entirely, whenever the other side is empty.
// mergeSecretsStepFirst must dedupe by EnvironmentVariable - step-level secrets
// win, then the first pipeline-level secret for a given environment variable -
// regardless of whether either input is empty.
func TestMergeSecretsStepFirst(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		stepSecrets     pipeline.Secrets
		pipelineSecrets pipeline.Secrets
		want            pipeline.Secrets
	}{
		{
			name: "both empty",
			want: nil,
		},
		{
			name:        "step empty, pipeline has duplicate env vars",
			stepSecrets: nil,
			pipelineSecrets: pipeline.Secrets{
				{Key: "first", EnvironmentVariable: "DB_PASSWORD"},
				{Key: "second", EnvironmentVariable: "DB_PASSWORD"},
			},
			want: pipeline.Secrets{{Key: "first", EnvironmentVariable: "DB_PASSWORD"}},
		},
		{
			name: "pipeline empty, step has duplicate env vars",
			stepSecrets: pipeline.Secrets{
				{Key: "first", EnvironmentVariable: "DB_PASSWORD"},
				{Key: "second", EnvironmentVariable: "DB_PASSWORD"},
			},
			pipelineSecrets: nil,
			want:            pipeline.Secrets{{Key: "first", EnvironmentVariable: "DB_PASSWORD"}},
		},
		{
			name: "both non-empty, step secret for an env var wins over the pipeline's",
			stepSecrets: pipeline.Secrets{
				{Key: "step-secret", EnvironmentVariable: "DB_PASSWORD"},
			},
			pipelineSecrets: pipeline.Secrets{
				{Key: "pipeline-secret", EnvironmentVariable: "DB_PASSWORD"},
				{Key: "other", EnvironmentVariable: "OTHER_VAR"},
			},
			want: pipeline.Secrets{
				{Key: "step-secret", EnvironmentVariable: "DB_PASSWORD"},
				{Key: "other", EnvironmentVariable: "OTHER_VAR"},
			},
		},
		{
			name: "secrets with no environment variable are dropped",
			pipelineSecrets: pipeline.Secrets{
				{Key: "no-env-var"},
				{Key: "has-env-var", EnvironmentVariable: "DB_PASSWORD"},
			},
			want: pipeline.Secrets{{Key: "has-env-var", EnvironmentVariable: "DB_PASSWORD"}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := mergeSecretsStepFirst(tc.stepSecrets, tc.pipelineSecrets)
			if diff := cmp.Diff(got, tc.want); diff != "" {
				t.Errorf("mergeSecretsStepFirst(%v, %v) (-got +want):\n%s", tc.stepSecrets, tc.pipelineSecrets, diff)
			}
		})
	}
}

// TestSignOfflineMergesPipelineLevelSecrets is a regression test for pipeline-level
// secrets not reaching command steps before signing, particularly ones nested inside
// group steps. It drives the real `tool sign` CLI command end to end: signing a
// pipeline whose secrets are declared only at the pipeline level, then verifying the
// resulting signatures.
func TestSignOfflineMergesPipelineLevelSecrets(t *testing.T) {
	// No t.Parallel(): this drives ToolSignCommand end to end via
	// `cmd := *ToolSignCommand`, a shallow copy that still shares the
	// package-level Command's underlying Flag objects. Running two such
	// invocations concurrently races on that shared flag state (see
	// TestSignOfflineDedupesDuplicatePipelineSecrets below), so - like every
	// other *_test.go in this package that reuses a package-level Command
	// this way - it must run sequentially relative to its sibling tests.

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

	for _, step := range []*pipeline.CommandStep{top, nested} {
		if len(step.Secrets) != 0 {
			t.Errorf("step %q secrets = %v, want none: an inherited pipeline-level secret shouldn't be persisted as an explicit step-level one", step.Label, step.Secrets)
		}
		if step.Signature == nil {
			t.Fatalf("step %q has no signature", step.Label)
		}
		if !slices.Contains(step.Signature.SignedFields, "secrets") {
			t.Errorf("step %q signed_fields = %v, want it to contain %q", step.Label, step.Signature.SignedFields, "secrets")
		}
	}

	// Verification needs to see the same pipeline-level secrets that were
	// merged in before signing, so re-merge them here, mirroring what
	// signOffline does before signing.
	mergePipelineSecretsIntoSteps(signedPipeline.Steps, signedPipeline.Secrets)

	for _, step := range []*pipeline.CommandStep{top, nested} {
		if err := signature.VerifyStep(t.Context(), step, pubSet, "fake-repo", signature.WithEnv(signedPipeline.Env.ToMap())); err != nil {
			t.Errorf("signature.VerifyStep(%q) error = %v, want success", step.Label, err)
		}
	}
}

// TestSignOfflineDedupesDuplicatePipelineSecrets is a regression test for two
// pipeline-level secrets targeting the same environment variable: signOffline
// must sign the same, deterministically-deduplicated set of secrets that
// verification later re-derives, not the raw (duplicate-containing) pipeline
// secrets list.
func TestSignOfflineDedupesDuplicatePipelineSecrets(t *testing.T) {
	// No t.Parallel(): see the comment in TestSignOfflineMergesPipelineLevelSecrets.

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
  - key: first-password
    environment_variable: DB_PASSWORD
  - key: second-password
    environment_variable: DB_PASSWORD
steps:
  - label: top-level
    command: echo top
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

	// Verification must normalize the same way signing did: the first
	// pipeline-level secret declared for DB_PASSWORD, not both.
	mergePipelineSecretsIntoSteps(signedPipeline.Steps, signedPipeline.Secrets)

	wantSecrets := pipeline.Secrets{{Key: "first-password", EnvironmentVariable: "DB_PASSWORD"}}
	if diff := cmp.Diff(top.Secrets, wantSecrets); diff != "" {
		t.Errorf("normalized step secrets (-got +want):\n%s", diff)
	}

	if err := signature.VerifyStep(t.Context(), top, pubSet, "fake-repo", signature.WithEnv(signedPipeline.Env.ToMap())); err != nil {
		t.Errorf("signature.VerifyStep() error = %v, want success", err)
	}
}
