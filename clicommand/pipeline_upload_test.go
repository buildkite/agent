package clicommand

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/env"
	"github.com/buildkite/agent/v4/internal/experiments"
	"github.com/buildkite/agent/v4/internal/reuse"
	"github.com/buildkite/agent/v4/logger"
	"github.com/buildkite/go-pipeline"
	"github.com/buildkite/go-pipeline/jwkutil"
	"github.com/buildkite/go-pipeline/ordered"
	"github.com/buildkite/go-pipeline/signature"
	"github.com/buildkite/go-pipeline/warning"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/urfave/cli/v3"
)

func TestPipelineUploadWithoutCheckout(t *testing.T) {
	t.Setenv("BUILDKITE_AGENT_JWKS_FILE", "")
	const commit = "0123456789abcdef0123456789abcdef01234567"
	for _, test := range []struct {
		name, skipCheckout string
		wantWarning        bool
	}{
		{"checkout skipped", "true", false},
		{"checkout not skipped", "false", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			t.Setenv("BUILDKITE_COMMIT", commit)
			t.Setenv("BUILDKITE_SKIP_CHECKOUT", test.skipCheckout)
			if err := os.WriteFile("pipeline.yml", []byte("steps:\n  - command: echo $BUILDKITE_COMMIT\n"), 0o600); err != nil {
				t.Fatal(err)
			}

			stderr, err := os.CreateTemp(t.TempDir(), "stderr")
			if err != nil {
				t.Fatal(err)
			}
			originalStderr := os.Stderr
			os.Stderr = stderr
			t.Cleanup(func() {
				os.Stderr = originalStderr
				_ = stderr.Close()
			})

			var stdout bytes.Buffer
			cmd := *PipelineUploadCommand
			app := &cli.Command{Name: "buildkite-agent", Commands: []*cli.Command{&cmd}, Writer: &stdout, ErrWriter: io.Discard}
			if err := app.Run(t.Context(), []string{"buildkite-agent", "upload", "--dry-run", "--agent-access-token", "unused-token", "pipeline.yml"}); err != nil {
				t.Fatalf("pipeline upload error = %v, want success", err)
			}

			var uploaded struct {
				Steps []struct{ Command string }
			}
			if err := json.Unmarshal(stdout.Bytes(), &uploaded); err != nil {
				t.Fatal(err)
			}
			if len(uploaded.Steps) != 1 || uploaded.Steps[0].Command != "echo "+commit {
				t.Errorf("uploaded pipeline = %s, want one command echoing %s", stdout.String(), commit)
			}
			output, err := os.ReadFile(stderr.Name())
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Contains(string(output), "Error running git rev-parse"); got != test.wantWarning {
				t.Errorf("stderr = %q, want commit resolution warning = %t", output, test.wantWarning)
			}
		})
	}
}

// TestPipelineUploadSigningMergesPipelineLevelSecrets is a regression test for
// pipeline-level secrets not reaching command steps before pipeline_upload.go
// signs them, particularly ones nested inside group steps. It also checks that
// the inherited pipeline-level secret is not persisted onto the uploaded step:
// merging it permanently would make the backend see an explicit step-level
// secret, which it can reject as conflicting with a step env var of the same
// name even though the original, unsigned pipeline was valid. This exercises
// pipeline_upload.go's own, independent signing path (as opposed to `tool sign`).
func TestPipelineUploadSigningMergesPipelineLevelSecrets(t *testing.T) {
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

	t.Chdir(t.TempDir())
	t.Setenv("BUILDKITE_REPO", "fake-repo")
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
	if err := os.WriteFile("pipeline.yml", []byte(pipelineYAML), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	cmd := *PipelineUploadCommand
	app := &cli.Command{Name: "buildkite-agent", Commands: []*cli.Command{&cmd}, Writer: &stdout, ErrWriter: io.Discard}
	if err := app.Run(t.Context(), []string{
		"buildkite-agent", "upload", "--dry-run",
		"--agent-access-token", "unused-token",
		"--jwks-file", jwksPath,
		"pipeline.yml",
	}); err != nil {
		t.Fatalf("pipeline upload error = %v, want success", err)
	}

	uploaded, err := pipeline.Parse(strings.NewReader(stdout.String()))
	if err != nil {
		t.Fatalf("parsing uploaded pipeline: %v\noutput:\n%s", err, stdout.String())
	}
	if len(uploaded.Steps) != 2 {
		t.Fatalf("uploaded.Steps = %+v, want 2 steps", uploaded.Steps)
	}

	top, ok := uploaded.Steps[0].(*pipeline.CommandStep)
	if !ok {
		t.Fatalf("Steps[0] = %T, want *pipeline.CommandStep", uploaded.Steps[0])
	}
	group, ok := uploaded.Steps[1].(*pipeline.GroupStep)
	if !ok {
		t.Fatalf("Steps[1] = %T, want *pipeline.GroupStep", uploaded.Steps[1])
	}
	if len(group.Steps) != 1 {
		t.Fatalf("group.Steps = %+v, want 1 nested step", group.Steps)
	}
	nested, ok := group.Steps[0].(*pipeline.CommandStep)
	if !ok {
		t.Fatalf("group.Steps[0] = %T, want *pipeline.CommandStep", group.Steps[0])
	}

	for _, step := range []*pipeline.CommandStep{top, nested} {
		// Neither step declares its own secrets in the source pipeline, so the
		// inherited pipeline-level secret must not be persisted onto the step
		// once it's been used for signing.
		if len(step.Secrets) != 0 {
			t.Errorf("step %q secrets = %+v, want none", step.Label, step.Secrets)
		}
		if step.Signature == nil {
			t.Fatalf("step %q has no signature", step.Label)
		}
		// The signature should still have been computed over the merged
		// (pipeline + step) secrets, so "secrets" is a signed field.
		if !slices.Contains(step.Signature.SignedFields, "secrets") {
			t.Errorf("step %q signed_fields = %v, want it to contain %q", step.Label, step.Signature.SignedFields, "secrets")
		}
	}

	// Verification needs to see the same merged pipeline-level secrets that
	// were present when pipeline_upload.go signed the steps, so re-merge
	// them here, mirroring what it does before signing.
	mergePipelineSecretsIntoSteps(uploaded.Steps, uploaded.Secrets)

	for _, step := range []*pipeline.CommandStep{top, nested} {
		if err := signature.VerifyStep(t.Context(), step, pubSet, "fake-repo", signature.WithEnv(uploaded.Env.ToMap())); err != nil {
			t.Errorf("signature.VerifyStep(%q) error = %v, want success", step.Label, err)
		}
	}
}

func TestPipelineUploadSignsArtifactPathsByDefault(t *testing.T) {
	t.Setenv("BUILDKITE_AGENT_JWKS_FILE", "")
	t.Setenv("BUILDKITE_REPO", "https://example.test/repo.git")
	t.Chdir(t.TempDir())
	if err := os.WriteFile("pipeline.yml", []byte("steps:\n  - command: echo hello\n    artifact_paths: logs/*.txt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	keys, _, err := jwkutil.NewKeyPair("test-key", jwa.EdDSA())
	if err != nil {
		t.Fatal(err)
	}
	keyFile, err := json.Marshal(keys)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("key.json", keyFile, 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	cmd := *PipelineUploadCommand
	app := &cli.Command{Name: "buildkite-agent", Commands: []*cli.Command{&cmd}, Writer: &stdout, ErrWriter: io.Discard}
	args := []string{"buildkite-agent", "upload", "--dry-run", "--agent-access-token", "unused-token", "--jwks-file", "key.json", "--jwks-key-id", "test-key", "pipeline.yml"}
	if err := app.Run(t.Context(), args); err != nil {
		t.Fatal(err)
	}
	var output struct {
		Steps []struct {
			Signature struct {
				SignedFields []string `json:"signed_fields"`
			} `json:"signature"`
		} `json:"steps"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("decoding signed pipeline: %v", err)
	}
	if len(output.Steps) != 1 {
		t.Fatalf("signed pipeline has %d steps, want 1", len(output.Steps))
	}
	if !slices.Contains(output.Steps[0].Signature.SignedFields, "artifact_paths") {
		t.Fatalf("signed fields = %v, want artifact_paths", output.Steps[0].Signature.SignedFields)
	}
}

func TestSearchForSecrets(t *testing.T) {
	t.Parallel()

	cfg := &PipelineUploadConfig{
		RedactedVars: []string{"SEKRET", "SSH_KEY"},
		AllowSecrets: false,
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
			desc:    "one step env secret within a trigger",
			environ: nil,
			pipeline: &pipeline.Pipeline{
				Steps: pipeline.Steps{
					&pipeline.TriggerStep{
						Contents: map[string]any{
							"build": ordered.MapFromItems(
								ordered.TupleSA{
									Key: "env", Value: ordered.MapFromItems(
										ordered.TupleSA{Key: "SEKRET", Value: "squirrels"},
										ordered.TupleSA{Key: "UNRELATED", Value: "horses"},
									),
								},
							),
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
		RedactedVars: []string{},
		AllowSecrets: false,
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
	ctx := t.Context()

	var gotPipelines []*pipeline.Pipeline

	for p, err := range cfg.parseAndInterpolate(ctx, "test", strings.NewReader(pipelineYAML), environ) {
		if err != nil {
			t.Errorf(`cfg.parseAndInterpolate(ctx, "test", %q, %v) = %v; want nil`, pipelineYAML, environ, err)
		}
		gotPipelines = append(gotPipelines, p)
	}
	if diff := cmp.Diff(gotPipelines, wantPipelines, cmp.Comparer(ordered.EqualSA), cmp.Comparer(ordered.EqualSS), cmpopts.IgnoreUnexported(pipeline.CommandStep{})); diff != "" {
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
				RedactedVars: []string{},
				AllowSecrets: false,
			}
			ctx := t.Context()
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

// TestParseWarningsSurviveInterpolation checks that a parse-level warning is reported as a *Warning by parseAndInterpolate
// regardless of whether interpolation is enabled.
func TestParseWarningsSurviveInterpolation(t *testing.T) {
	t.Parallel()
	const pipelineYAML = `---
steps:
- llamas
`

	tests := []struct {
		name          string
		interpolation bool
	}{
		{name: "with interpolation", interpolation: true},
		{name: "without interpolation", interpolation: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			cfg := &PipelineUploadConfig{
				NoInterpolation: !test.interpolation,
				RedactedVars:    []string{},
			}
			ctx := t.Context()
			environ := env.FromMap(map[string]string{})

			var gotWarning bool
			for _, err := range cfg.parseAndInterpolate(ctx, "test", strings.NewReader(pipelineYAML), environ) {
				if err == nil {
					continue
				}
				if w := warning.As(err); w != nil {
					gotWarning = true
					continue
				}
				t.Errorf("parseAndInterpolate yielded a non-warning error = %v; want a *Warning", err)
			}

			if !gotWarning {
				t.Errorf("parseAndInterpolate(interpolation=%v) yielded no warning for %q; want a parse warning", test.interpolation, pipelineYAML)
			}
		})
	}
}

// TestHandleParseError covers the bail-out decision for errors yielded by
// parseAndInterpolate: hard errors always abort, warnings abort only when
// --reject-parse-warnings is set, and otherwise warnings are logged and the
// upload proceeds.
func TestHandleParseError(t *testing.T) {
	t.Parallel()

	parseWarning := warning.Newf("unknown step type %q", "llamas")
	hardError := errors.New("pipeline parsing failed")

	tests := []struct {
		desc                string
		rejectParseWarnings bool
		err                 error
		wantAbort           bool
		wantExitCode        int
		wantLogContains     string
	}{
		{
			desc:      "nil error proceeds",
			err:       nil,
			wantAbort: false,
		},
		{
			desc:      "hard error aborts regardless of flag",
			err:       hardError,
			wantAbort: true,
		},
		{
			desc:                "hard error aborts even with reject enabled",
			err:                 hardError,
			rejectParseWarnings: true,
			wantAbort:           true,
		},
		{
			desc:            "warning is logged and proceeds by default",
			err:             parseWarning,
			wantAbort:       false,
			wantLogContains: "pipeline upload will proceed",
		},
		{
			desc:                "warning aborts when reject enabled",
			err:                 parseWarning,
			rejectParseWarnings: true,
			wantAbort:           true,
			wantExitCode:        1,
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			cfg := &PipelineUploadConfig{RejectParseWarnings: test.rejectParseWarnings}
			l := logger.NewBuffer()

			got := cfg.handleParseError(l, "test.yaml", test.err)

			if test.wantAbort && got == nil {
				t.Fatalf("handleParseError(...) = nil; want non-nil (abort)")
			}
			if !test.wantAbort && got != nil {
				t.Fatalf("handleParseError(...) = %v; want nil (proceed)", got)
			}

			if test.wantExitCode != 0 {
				var exitErr *ExitError
				if !errors.As(got, &exitErr) {
					t.Fatalf("handleParseError(...) = %v; want an *ExitError", got)
				}
				if exitErr.Code() != test.wantExitCode {
					t.Errorf("ExitError code = %d; want %d", exitErr.Code(), test.wantExitCode)
				}
			}

			var loggedWarning string
			for _, m := range l.Messages {
				if strings.HasPrefix(m, "[warn] ") {
					loggedWarning = m
				}
			}
			switch {
			case test.wantLogContains == "" && loggedWarning != "":
				t.Errorf("unexpected warning logged: %q", loggedWarning)
			case test.wantLogContains != "" && !strings.Contains(loggedWarning, test.wantLogContains):
				t.Errorf("warning log = %q; want it to contain %q", loggedWarning, test.wantLogContains)
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
				AllowSecrets:    false,
			}
			ctx := t.Context()

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

func TestIfChangedApplicator(t *testing.T) {
	t.Parallel()

	makeInput := func() pipeline.Steps {
		return pipeline.Steps{
			&pipeline.CommandStep{
				Command: "always runs",
			},
			&pipeline.CommandStep{
				Command: "only runs when files in foo changed",
				RemainingFields: map[string]any{
					"if_changed": "foo/**",
				},
			},
			&pipeline.CommandStep{
				Command: "only runs when files in foo changed, except for baz",
				RemainingFields: map[string]any{
					"if_changed": ordered.MapFromItems(
						ordered.TupleSA{Key: "include", Value: "foo/**"},
						ordered.TupleSA{Key: "exclude", Value: "foo/baz"},
					),
				},
			},
			&pipeline.CommandStep{
				Command: "only runs when files in bar changed",
				RemainingFields: map[string]any{
					"if_changed": "bar/**",
				},
			},
			&pipeline.CommandStep{
				Command: "only runs when files in foo or bar changed",
				RemainingFields: map[string]any{
					"if_changed": []any{"foo/**", "bar/**"},
				},
			},
			&pipeline.CommandStep{
				Command: "only runs when any files changed",
				RemainingFields: map[string]any{
					"if_changed": "**",
				},
			},
		}
	}

	tests := []struct {
		name string
		ica  *ifChangedApplicator
		want pipeline.Steps
	}{
		{
			name: "disabled",
			ica: &ifChangedApplicator{
				enabled: false,
			},
			want: pipeline.Steps{
				&pipeline.CommandStep{Command: "always runs"},
				&pipeline.CommandStep{
					Command:         "only runs when files in foo changed",
					RemainingFields: map[string]any{},
				},
				&pipeline.CommandStep{
					Command:         "only runs when files in foo changed, except for baz",
					RemainingFields: map[string]any{},
				},
				&pipeline.CommandStep{
					Command:         "only runs when files in bar changed",
					RemainingFields: map[string]any{},
				},
				&pipeline.CommandStep{
					Command:         "only runs when files in foo or bar changed",
					RemainingFields: map[string]any{},
				},
				&pipeline.CommandStep{
					Command:         "only runs when any files changed",
					RemainingFields: map[string]any{},
				},
			},
		},
		{
			name: "no changes",
			ica: &ifChangedApplicator{
				enabled:      true,
				gathered:     true, // pretend we ran git diff
				changedPaths: nil,
			},
			want: pipeline.Steps{
				&pipeline.CommandStep{Command: "always runs"},
				&pipeline.CommandStep{
					Command: "only runs when files in foo changed",
					RemainingFields: map[string]any{
						"skip": ifChangedSkippedMsg,
					},
				},
				&pipeline.CommandStep{
					Command: "only runs when files in foo changed, except for baz",
					RemainingFields: map[string]any{
						"skip": ifChangedSkippedMsg,
					},
				},
				&pipeline.CommandStep{
					Command: "only runs when files in bar changed",
					RemainingFields: map[string]any{
						"skip": ifChangedSkippedMsg,
					},
				},
				&pipeline.CommandStep{
					Command: "only runs when files in foo or bar changed",
					RemainingFields: map[string]any{
						"skip": ifChangedSkippedMsg,
					},
				},
				&pipeline.CommandStep{
					Command: "only runs when any files changed",
					RemainingFields: map[string]any{
						"skip": ifChangedSkippedMsg,
					},
				},
			},
		},
		{
			name: "change in qux",
			ica: &ifChangedApplicator{
				enabled:      true,
				gathered:     true, // pretend we ran git diff
				changedPaths: []string{"qux"},
			},
			want: pipeline.Steps{
				&pipeline.CommandStep{Command: "always runs"},
				&pipeline.CommandStep{
					Command: "only runs when files in foo changed",
					RemainingFields: map[string]any{
						"skip": ifChangedSkippedMsg,
					},
				},
				&pipeline.CommandStep{
					Command: "only runs when files in foo changed, except for baz",
					RemainingFields: map[string]any{
						"skip": ifChangedSkippedMsg,
					},
				},
				&pipeline.CommandStep{
					Command: "only runs when files in bar changed",
					RemainingFields: map[string]any{
						"skip": ifChangedSkippedMsg,
					},
				},
				&pipeline.CommandStep{
					Command: "only runs when files in foo or bar changed",
					RemainingFields: map[string]any{
						"skip": ifChangedSkippedMsg,
					},
				},
				&pipeline.CommandStep{
					Command:         "only runs when any files changed",
					RemainingFields: map[string]any{},
				},
			},
		},
		{
			name: "change in foo/README.md",
			ica: &ifChangedApplicator{
				enabled:      true,
				gathered:     true, // pretend we ran git diff
				changedPaths: []string{"foo/README.md"},
			},
			want: pipeline.Steps{
				&pipeline.CommandStep{Command: "always runs"},
				&pipeline.CommandStep{
					Command:         "only runs when files in foo changed",
					RemainingFields: map[string]any{},
				},
				&pipeline.CommandStep{
					Command:         "only runs when files in foo changed, except for baz",
					RemainingFields: map[string]any{},
				},
				&pipeline.CommandStep{
					Command: "only runs when files in bar changed",
					RemainingFields: map[string]any{
						"skip": ifChangedSkippedMsg,
					},
				},
				&pipeline.CommandStep{
					Command:         "only runs when files in foo or bar changed",
					RemainingFields: map[string]any{},
				},
				&pipeline.CommandStep{
					Command:         "only runs when any files changed",
					RemainingFields: map[string]any{},
				},
			},
		},

		{
			name: "change in foo/baz",
			ica: &ifChangedApplicator{
				enabled:      true,
				gathered:     true, // pretend we ran git diff
				changedPaths: []string{"foo/baz"},
			},
			want: pipeline.Steps{
				&pipeline.CommandStep{Command: "always runs"},
				&pipeline.CommandStep{
					Command:         "only runs when files in foo changed",
					RemainingFields: map[string]any{},
				},
				&pipeline.CommandStep{
					Command: "only runs when files in foo changed, except for baz",
					RemainingFields: map[string]any{
						"skip": ifChangedSkippedMsg,
					},
				},
				&pipeline.CommandStep{
					Command: "only runs when files in bar changed",
					RemainingFields: map[string]any{
						"skip": ifChangedSkippedMsg,
					},
				},
				&pipeline.CommandStep{
					Command:         "only runs when files in foo or bar changed",
					RemainingFields: map[string]any{},
				},
				&pipeline.CommandStep{
					Command:         "only runs when any files changed",
					RemainingFields: map[string]any{},
				},
			},
		},
		{
			name: "change in bar/README.md",
			ica: &ifChangedApplicator{
				enabled:      true,
				gathered:     true, // pretend we ran git diff
				changedPaths: []string{"bar/README.md"},
			},
			want: pipeline.Steps{
				&pipeline.CommandStep{Command: "always runs"},
				&pipeline.CommandStep{
					Command: "only runs when files in foo changed",
					RemainingFields: map[string]any{
						"skip": ifChangedSkippedMsg,
					},
				},
				&pipeline.CommandStep{
					Command: "only runs when files in foo changed, except for baz",
					RemainingFields: map[string]any{
						"skip": ifChangedSkippedMsg,
					},
				},
				&pipeline.CommandStep{
					Command:         "only runs when files in bar changed",
					RemainingFields: map[string]any{},
				},
				&pipeline.CommandStep{
					Command:         "only runs when files in foo or bar changed",
					RemainingFields: map[string]any{},
				},
				&pipeline.CommandStep{
					Command:         "only runs when any files changed",
					RemainingFields: map[string]any{},
				},
			},
		},
		{
			name: "changes in foo/hello.go, foo/baz, and bar/README.md",
			ica: &ifChangedApplicator{
				enabled:      true,
				gathered:     true, // pretend we ran git diff
				changedPaths: []string{"foo/hello.go", "foo/baz", "bar/README.md"},
			},
			want: pipeline.Steps{
				&pipeline.CommandStep{Command: "always runs"},
				&pipeline.CommandStep{
					Command:         "only runs when files in foo changed",
					RemainingFields: map[string]any{},
				},
				&pipeline.CommandStep{
					// A file other than baz changed in foo, so this runs.
					Command:         "only runs when files in foo changed, except for baz",
					RemainingFields: map[string]any{},
				},
				&pipeline.CommandStep{
					Command:         "only runs when files in bar changed",
					RemainingFields: map[string]any{},
				},
				&pipeline.CommandStep{
					Command:         "only runs when files in foo or bar changed",
					RemainingFields: map[string]any{},
				},
				&pipeline.CommandStep{
					Command:         "only runs when any files changed",
					RemainingFields: map[string]any{},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			l := logger.NewConsoleLogger(logger.NewTestPrinter(t), func(i int) { t.Errorf("exitFn(%d) invoked", i) })

			steps := makeInput()
			test.ica.apply(l, steps)
			if diff := cmp.Diff(steps, test.want, cmpopts.IgnoreUnexported(pipeline.CommandStep{})); diff != "" {
				t.Errorf("after ica.apply(l, steps) (-got, +want):\n%s", diff)
			}
		})
	}
}

func TestIfChangedApplicator_WeirdPipeline(t *testing.T) {
	t.Parallel()

	steps := pipeline.Steps{
		&pipeline.CommandStep{
			Command: "unsupported type for if_changed",
			RemainingFields: map[string]any{
				"if_changed": 42,
			},
		},
		&pipeline.CommandStep{
			Command: "invalid glob pattern",
			RemainingFields: map[string]any{
				"if_changed": "bar/**/[asdf[[[[asdf",
			},
		},
		&pipeline.CommandStep{
			Command: "invalid exclude pattern",
			RemainingFields: map[string]any{
				"if_changed": ordered.MapFromItems(
					ordered.TupleSA{Key: "include", Value: "**"},
					ordered.TupleSA{Key: "exclude", Value: "{a{b{c{d"},
				),
			},
		},
		&pipeline.CommandStep{
			Command: "mapping without include",
			RemainingFields: map[string]any{
				"if_changed": ordered.MapFromItems(
					ordered.TupleSA{Key: "exclude", Value: "asdf"},
				),
			},
		},
		&pipeline.TriggerStep{
			Contents: map[string]any{
				"if_changed": "version/VERSION",
			},
		},
		&pipeline.GroupStep{
			Steps: pipeline.Steps{
				&pipeline.CommandStep{
					Command: "doesn't matter, it's in a group",
					RemainingFields: map[string]any{
						"if_changed": "**",
					},
				},
			},
			RemainingFields: map[string]any{
				"if_changed": "CHANGELOG.md",
			},
		},
	}

	want := pipeline.Steps{
		&pipeline.CommandStep{
			Command:         "unsupported type for if_changed",
			RemainingFields: map[string]any{},
		},
		&pipeline.CommandStep{
			Command:         "invalid glob pattern",
			RemainingFields: map[string]any{},
		},
		&pipeline.CommandStep{
			Command:         "invalid exclude pattern",
			RemainingFields: map[string]any{},
		},
		&pipeline.CommandStep{
			Command:         "mapping without include",
			RemainingFields: map[string]any{},
		},
		&pipeline.TriggerStep{
			Contents: map[string]any{
				"skip": ifChangedSkippedMsg,
			},
		},
		&pipeline.GroupStep{
			Steps: pipeline.Steps{
				&pipeline.CommandStep{
					Command:         "doesn't matter, it's in a group",
					RemainingFields: map[string]any{},
				},
			},
			RemainingFields: map[string]any{
				"skip": ifChangedSkippedMsg,
			},
		},
	}

	l := logger.NewConsoleLogger(logger.NewTestPrinter(t), func(i int) { t.Errorf("exitFn(%d) invoked", i) })

	ica := &ifChangedApplicator{
		enabled:      true,
		gathered:     true,
		changedPaths: []string{"foo/happy.jpg"},
	}

	ica.apply(l, steps)
	if diff := cmp.Diff(steps, want, cmpopts.IgnoreUnexported(pipeline.CommandStep{})); diff != "" {
		t.Errorf("after ica.apply(l, steps) (-got, +want):\n%s", diff)
	}
}

func TestReadChangedFilesFromPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		want    []string
	}{
		{
			name:    "single file",
			content: "foo/bar.go\n",
			want:    []string{"foo/bar.go"},
		},
		{
			name:    "multiple files",
			content: "foo/bar.go\nsrc/main.go\nREADME.md\n",
			want:    []string{"foo/bar.go", "src/main.go", "README.md"},
		},
		{
			name:    "empty lines filtered",
			content: "foo/bar.go\n\nsrc/main.go\n\n",
			want:    []string{"foo/bar.go", "src/main.go"},
		},
		{
			name:    "no trailing newline",
			content: "foo/bar.go\nsrc/main.go",
			want:    []string{"foo/bar.go", "src/main.go"},
		},
		{
			name:    "empty file",
			content: "",
			want:    []string{},
		},
		{
			name:    "only newlines",
			content: "\n\n\n",
			want:    []string{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			tmpFile, err := os.CreateTemp("", "changed-files-*.txt")
			if err != nil {
				t.Fatalf("creating temp file: %v", err)
			}
			defer func() { _ = os.Remove(tmpFile.Name()) }()

			if _, err := tmpFile.WriteString(test.content); err != nil {
				t.Fatalf("writing to temp file: %v", err)
			}
			if err := tmpFile.Close(); err != nil {
				t.Fatalf("tmpFile.Close() = %v", err)
			}

			l := logger.NewBuffer()
			got, err := readChangedFilesFromPath(l, tmpFile.Name())
			if err != nil {
				t.Fatalf("readChangedFilesFromPath() error = %v", err)
			}

			if diff := cmp.Diff(got, test.want); diff != "" {
				t.Errorf("readChangedFilesFromPath() diff (-got +want):\n%s", diff)
			}
		})
	}
}

func TestIfChangedApplicator_WithChangedFilesPath(t *testing.T) {
	t.Parallel()

	// Create a temp file with changed files
	tmpFile, err := os.CreateTemp("", "changed-files-*.txt")
	if err != nil {
		t.Fatalf("creating temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	if _, err := tmpFile.WriteString("foo/README.md\nbar/test.go\n"); err != nil {
		t.Fatalf("writing to temp file: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("tmpFile.Close() = %v", err)
	}

	steps := pipeline.Steps{
		&pipeline.CommandStep{
			Command: "runs when foo changes",
			RemainingFields: map[string]any{
				"if_changed": "foo/**",
			},
		},
		&pipeline.CommandStep{
			Command: "runs when qux changes",
			RemainingFields: map[string]any{
				"if_changed": "qux/**",
			},
		},
	}

	want := pipeline.Steps{
		&pipeline.CommandStep{
			Command:         "runs when foo changes",
			RemainingFields: map[string]any{},
		},
		&pipeline.CommandStep{
			Command: "runs when qux changes",
			RemainingFields: map[string]any{
				"skip": ifChangedSkippedMsg,
			},
		},
	}

	l := logger.NewConsoleLogger(logger.NewTestPrinter(t), func(i int) { t.Errorf("exitFn(%d) invoked", i) })

	ica := &ifChangedApplicator{
		enabled:          true,
		changedFilesPath: tmpFile.Name(),
	}

	ica.apply(l, steps)
	if diff := cmp.Diff(steps, want, cmpopts.IgnoreUnexported(pipeline.CommandStep{})); diff != "" {
		t.Errorf("after ica.apply(l, steps) (-got, +want):\n%s", diff)
	}
}

func TestPipelineUploadFailuresAreCaptured(t *testing.T) {
	agentAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"Step 1 is missing a command"}`))
	}))
	t.Cleanup(agentAPI.Close)

	for _, test := range []struct {
		name, pipeline, doing, message string
		args                           []string
		fallback                       bool
		excludes                       string
	}{
		{
			name:     "parse",
			pipeline: "steps:\n  - command: [unterminated\n",
			doing:    "parsing the pipeline",
			message:  `pipeline parsing of "pipeline.yml" failed`,
		},
		{
			name:     "secret detection",
			pipeline: "steps:\n  - command: echo $UPLOAD_TEST_SECRET\n",
			doing:    "checking the pipeline for secrets",
			message:  "UPLOAD_TEST_SECRET",
			args:     []string{"--redacted-vars", "UPLOAD_TEST_SECRET"},
		},
		{
			// The parent agent does not know a value set only in the step's
			// shell, so the command redacts it.
			name:     "interpolation error naming a secret",
			pipeline: "steps:\n  - command: echo ${MISSING_VALUE?the value is $UPLOAD_TEST_SECRET}\n",
			doing:    "parsing the pipeline",
			message:  "the value is [REDACTED]",
			args:     []string{"--redacted-vars", "UPLOAD_TEST_SECRET"},
		},
		{
			// Without usable patterns nothing can be redacted, so only the
			// agent's own text is sent.
			name:     "invalid redacted-vars pattern",
			pipeline: "steps:\n  - command: echo ${MISSING_VALUE?the value is $UPLOAD_TEST_SECRET}\n",
			doing:    "parsing the pipeline",
			message:  "Fix the pipeline definition",
			args:     []string{"--redacted-vars", "[UPLOAD_TEST_SECRET"},
			fallback: true,
		},
		{
			// The pipeline's env block adds the value only to the
			// interpolation environment, which the parent agent never sees.
			name:     "interpolation error naming a pipeline env secret",
			pipeline: "env:\n  DEPLOY_SECRET: sensitive-value\nsteps:\n  - command: echo ${MISSING_VALUE?the value is $DEPLOY_SECRET}\n",
			doing:    "parsing the pipeline",
			message:  "the value is [REDACTED]",
			args:     []string{"--redacted-vars", "*_SECRET"},
			excludes: "sensitive-value",
		},
		{
			name:     "missing job",
			pipeline: "steps:\n  - command: echo hello\n",
			doing:    "checking the job's environment",
			message:  "missing job parameter",
			args:     []string{"--job", ""},
		},
		{
			name:     "upload",
			pipeline: "steps:\n  - command: echo hello\n",
			doing:    "uploading the pipeline to Buildkite",
			message:  "Step 1 is missing a command",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			reports := startAgentErrorTestServer(t)
			t.Setenv("UPLOAD_TEST_SECRET", "a-very-secret-value")
			path := filepath.Join(t.TempDir(), "pipeline.yml")
			if err := os.WriteFile(path, []byte(test.pipeline), 0o600); err != nil {
				t.Fatal(err)
			}

			cmd := *PipelineUploadCommand
			app := &cli.Command{Name: "buildkite-agent", Commands: []*cli.Command{&cmd}, Writer: io.Discard, ErrWriter: io.Discard}
			args := append([]string{
				"buildkite-agent", "upload",
				"--endpoint", agentAPI.URL, "--agent-access-token", "job-token", "--job", "job-id",
			}, test.args...)
			if err := app.Run(t.Context(), append(args, path)); err == nil {
				t.Fatal("pipeline upload succeeded, want failure")
			}

			if len(*reports) != 1 {
				t.Fatalf("reports = %+v, want one", *reports)
			}
			report := (*reports)[0]
			want := "`buildkite-agent pipeline upload` failed while " + test.doing + ": "
			if test.fallback {
				want = "`buildkite-agent pipeline upload` failed while " + test.doing + ". "
			}
			if report.Code != "buildkite.pipeline_upload_failed" || !strings.HasPrefix(report.Message, want) {
				t.Errorf("report = %q %q, want buildkite.pipeline_upload_failed starting %q", report.Code, report.Message, want)
			}
			if !strings.Contains(report.Message, test.message) || strings.Contains(report.Message, "a-very-secret-value") || (test.excludes != "" && strings.Contains(report.Message, test.excludes)) {
				t.Errorf("message = %q, want it to contain %q and no secret value", report.Message, test.message)
			}
		})
	}
}

func TestPipelineUploadFix(t *testing.T) {
	t.Parallel()
	apiError := func(status int) error {
		return &api.ErrorResponse{Response: &http.Response{StatusCode: status}}
	}
	for _, test := range []struct {
		name string
		err  error
		want string
	}{
		{"pipeline rejected", apiError(http.StatusUnprocessableEntity), "Fix the pipeline definition so that Buildkite accepts it."},
		{"token rejected", apiError(http.StatusUnauthorized), "Buildkite rejected the job's agent access token."},
		{"server error", apiError(http.StatusServiceUnavailable), "Retry the step."},
		{"rate limited", apiError(http.StatusTooManyRequests), "Buildkite rate limited the upload."},
		{"not found", apiError(http.StatusNotFound), "If Buildkite rejected the pipeline, fix the pipeline definition"},
		{"transport or processing failure", errors.New("connection refused"), "If Buildkite rejected the pipeline, fix the pipeline definition"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := pipelineUploadFix(fmt.Errorf("failed to upload: %w", test.err)); !strings.Contains(got, test.want) {
				t.Errorf("pipelineUploadFix() = %q, want it to contain %q", got, test.want)
			}
		})
	}
}

func TestPipelineUploadReuseInputsDigest(t *testing.T) {
	dir := t.TempDir()
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	runGit("init", "-q")
	for name, content := range map[string]string{
		"package.json":  "{}",
		"yarn.lock":     "lock",
		"web/app.ts":    "app",
		"web/README.md": "readme",
		"tsconfig.json": "{}",
		"unrelated.txt": "x",
	} {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runGit("add", "-A")
	runGit("commit", "-q", "-m", "init")
	t.Chdir(dir)

	const yml = `steps:
  - key: deps
    command: ./install.sh
    inputs:
      files: ["package.json", "yarn.lock"]
    outputs:
      paths: ["node_modules/"]
    reuse: true
  - group: g
    steps:
      - key: build
        command: ./build.sh
        inputs:
          files:
            include: ["web/**", "tsconfig.json"]
            exclude: ["web/**/*.md"]
          steps: [deps]
        reuse: true
  - key: nothing
    command: ./x.sh
    inputs:
      files: ["package.json", "nope/**"]
  - key: plain
    command: ./y.sh
`
	p, err := pipeline.Parse(strings.NewReader(yml))
	if err != nil {
		t.Fatalf("pipeline.Parse() error = %v", err)
	}

	l := logger.NewConsoleLogger(logger.NewTestPrinter(t), func(i int) { t.Errorf("exitFn(%d) invoked", i) })
	(&inputsDigestApplicator{hasher: new(reuse.Hasher)}).apply(t.Context(), l, p.Steps)

	digestOf := func(s *pipeline.CommandStep) map[string]any {
		t.Helper()
		m, ok := s.RemainingFields["inputs_digest"].(*ordered.MapSA)
		if !ok {
			t.Fatalf("step %q inputs_digest = %#v, want a map", s.Key, s.RemainingFields["inputs_digest"])
		}
		return m.ToMap()
	}

	deps := p.Steps[0].(*pipeline.CommandStep)
	build := p.Steps[1].(*pipeline.GroupStep).Steps[0].(*pipeline.CommandStep)
	nothing := p.Steps[2].(*pipeline.CommandStep)
	plain := p.Steps[3].(*pipeline.CommandStep)

	for _, tc := range []struct {
		step      *pipeline.CommandStep
		include   []string
		exclude   []string
		wantCount int
	}{
		{deps, []string{"package.json", "yarn.lock"}, nil, 2},
		{build, []string{"web/**", "tsconfig.json"}, []string{"web/**/*.md"}, 2},
	} {
		want, err := reuse.HashFiles(t.Context(), ".", tc.include, tc.exclude, false)
		if err != nil {
			t.Fatal(err)
		}
		wantMap := map[string]any{"algorithm": "blake3", "files_digest": want.Digest, "files_count": tc.wantCount}
		if diff := cmp.Diff(digestOf(tc.step), wantMap); diff != "" {
			t.Errorf("step %q inputs_digest (-got +want):\n%s", tc.step.Key, diff)
		}
		for _, key := range []string{"inputs", "reuse"} {
			if _, ok := tc.step.RemainingFields[key]; !ok {
				t.Errorf("step %q lost %q", tc.step.Key, key)
			}
		}
	}

	// A pattern matching nothing is an error even when another matches.
	if diff := cmp.Diff(digestOf(nothing), map[string]any{"error": `inputs.files pattern "nope/**" matched no files`}); diff != "" {
		t.Errorf("step nothing inputs_digest (-got +want):\n%s", diff)
	}
	if _, ok := plain.RemainingFields["inputs_digest"]; ok {
		t.Errorf("step plain has inputs_digest, want none")
	}
}

func TestPipelineUploadSignsReuseDeclarations(t *testing.T) {
	t.Setenv("BUILDKITE_AGENT_JWKS_FILE", "")
	t.Setenv("BUILDKITE_REPO", "https://example.test/repo.git")
	dir := t.TempDir()
	cmd := exec.Command("git", "init", "-q")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	t.Chdir(dir)
	const yml = `steps:
  - key: deps
    command: ./install.sh
    outputs:
      paths: ["node_modules/"]
    reuse: true
  - group: g
    steps:
      - key: build
        command: ./build.sh
        inputs:
          steps: [deps]
        reuse:
          scope: pipeline
  - key: plain
    command: ./y.sh
`
	if err := os.WriteFile("pipeline.yml", []byte(yml), 0o600); err != nil {
		t.Fatal(err)
	}
	priv, pub, err := jwkutil.NewKeyPair("test-key", jwa.EdDSA())
	if err != nil {
		t.Fatal(err)
	}
	keyFile, err := json.Marshal(priv)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("key.json", keyFile, 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	upload := *PipelineUploadCommand
	app := &cli.Command{Name: "buildkite-agent", Commands: []*cli.Command{&upload}, Writer: &stdout, ErrWriter: io.Discard}
	args := []string{"buildkite-agent", "upload", "--dry-run", "--agent-access-token", "unused-token", "--jwks-file", "key.json", "--jwks-key-id", "test-key", "pipeline.yml"}
	if err := app.Run(t.Context(), args); err != nil {
		t.Fatal(err)
	}

	// Decode each command step as a job's step is decoded.
	var output struct {
		Steps []json.RawMessage `json:"steps"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("decoding signed pipeline: %v", err)
	}
	var group struct {
		Steps []json.RawMessage `json:"steps"`
	}
	if err := json.Unmarshal(output.Steps[1], &group); err != nil {
		t.Fatal(err)
	}
	decode := func(raw json.RawMessage) *pipeline.CommandStep {
		t.Helper()
		step := new(pipeline.CommandStep)
		if err := json.Unmarshal(raw, step); err != nil {
			t.Fatal(err)
		}
		return step
	}
	deps, build, plain := decode(output.Steps[0]), decode(group.Steps[0]), decode(output.Steps[2])

	for _, step := range []*pipeline.CommandStep{deps, build} {
		sig, ok := step.RemainingFields[reuse.SignatureField].(*ordered.MapSA)
		if !ok {
			t.Fatalf("step %q reuse_signature = %#v, want a map", step.Key, step.RemainingFields[reuse.SignatureField])
		}
		if alg, _ := sig.Get("algorithm"); alg != "EdDSA" {
			t.Errorf("step %q reuse_signature.algorithm = %v, want EdDSA", step.Key, alg)
		}
		if err := reuse.VerifyDeclaration(pub, step.Signature.Value, step.RemainingFields); err != nil {
			t.Errorf("step %q VerifyDeclaration() = %v", step.Key, err)
		}
	}
	if _, ok := plain.RemainingFields[reuse.SignatureField]; ok {
		t.Errorf("step plain has reuse_signature, want none")
	}

	// Changing the declaration, or moving it to another step, fails.
	build.RemainingFields["inputs"] = map[string]any{"steps": []any{"other"}}
	if err := reuse.VerifyDeclaration(pub, build.Signature.Value, build.RemainingFields); !errors.Is(err, reuse.ErrDeclarationMismatch) {
		t.Errorf("VerifyDeclaration() with tampered inputs = %v, want %v", err, reuse.ErrDeclarationMismatch)
	}
	if err := reuse.VerifyDeclaration(pub, plain.Signature.Value, deps.RemainingFields); !errors.Is(err, reuse.ErrDeclarationMismatch) {
		t.Errorf("VerifyDeclaration() with another step's signature = %v, want %v", err, reuse.ErrDeclarationMismatch)
	}
}
