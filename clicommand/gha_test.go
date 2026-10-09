package clicommand

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/logger"
	gha "github.com/buildkite/buildkite-gha"
	"github.com/google/go-cmp/cmp"
	"github.com/urfave/cli/v3"
	"gopkg.in/yaml.v3"
)

const ghaTestJob = "11111111-2222-4333-8444-555555555555"

// Exercise the real executable as both importer and content-addressed runtime.
// A test executable used as the distribution would not prove argv dispatch.
func TestGitHubActionsUploadAndRuntime(t *testing.T) {
	if runtime.GOOS != "linux" && (runtime.GOOS != "darwin" || runtime.GOARCH != "arm64") {
		t.Skip("GHA importer supports Linux and macOS arm64")
	}
	repo, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	executable := filepath.Join(bin, "buildkite-agent")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", executable, ".")
	build.Dir = repo
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build agent: %v\n%s", err, output)
	}

	root := t.TempDir()
	t.Chdir(root)
	workflow := `on: push
jobs:
  plan:
    runs-on: ubuntu-latest
    outputs:
      matrix: ${{ steps.matrix.outputs.matrix }}
    steps:
      - id: matrix
        run: echo 'matrix=[{"target":"first"},{"target":"second"}]' >> "$GITHUB_OUTPUT"
  build:
    needs: plan
    runs-on: ubuntu-latest
    strategy:
      matrix:
        include: ${{ fromJSON(needs.plan.outputs.matrix) }}
    steps:
      - run: echo "matrix-target=${{ matrix.target }}"
  tolerated:
    runs-on: ubuntu-latest
    continue-on-error: true
    steps:
      - run: exit 7
`
	if runtime.GOOS == "darwin" {
		workflow = strings.ReplaceAll(workflow, "ubuntu-latest", "macos-latest")
	}
	if err := os.WriteFile("ci.yml", []byte(workflow), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "ci.yml"}, {"-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "Add workflow"}} {
		if output, err := exec.CommandContext(t.Context(), "git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}
	commit, err := exec.CommandContext(t.Context(), "git", "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	sha := strings.TrimSpace(string(commit))
	event := fmt.Sprintf(`{"provider":"github","event":"push","repository":{"owner":"owner","name":"repo","clone_url":"https://github.com/owner/repo.git","default_branch":"main"},"ref":"refs/heads/main","sha":%q,"actor":"test","payload":{"ref":"refs/heads/main"}}`, sha)
	if err := os.WriteFile("event.json", []byte(event), 0o600); err != nil {
		t.Fatal(err)
	}

	store := &ghaTestAPI{t: t, artifacts: make(map[string]*api.Artifact), contents: make(map[string][]byte), finished: make(map[string]bool), jobs: make(map[string]string)}
	server := httptest.NewServer(store)
	defer server.Close()
	store.url = server.URL
	for key, value := range map[string]string{
		"BUILDKITE": "true", "BUILDKITE_JOB_ID": ghaTestJob, "BUILDKITE_STEP_KEY": "importer",
		"BUILDKITE_STEP_ID":  "33333333-2222-4333-8444-555555555555",
		"BUILDKITE_BUILD_ID": "22222222-2222-4333-8444-555555555555", "BUILDKITE_BUILD_NUMBER": "1",
		"BUILDKITE_COMMIT": sha, "BUILDKITE_BRANCH": "main", "BUILDKITE_REPO": "https://github.com/owner/repo.git",
		"BUILDKITE_BUILD_CHECKOUT_PATH": root, "BUILDKITE_AGENT_META_DATA_QUEUE": "gha-poc",
		"BUILDKITE_AGENT_ENDPOINT": server.URL, "BUILDKITE_AGENT_ACCESS_TOKEN": "test-job-token",
		"BUILDKITE_GHA_TELEMETRY_DISABLED": "true", "BUILDKITE_GHA_PLAN_DIGEST": "",
		"BUILDKITE_REQUEST_HEADER_BUILDKITE_TEST":           "gha",
		"BUILDKITE_USE_REPOSITORY_PROVIDER_GIT_CREDENTIALS": "", "BUILDKITE_USE_GITHUB_APP_GIT_CREDENTIALS": "",
		"PATH": bin + string(os.PathListSeparator) + os.Getenv("PATH"),
	} {
		t.Setenv(key, value)
	}
	args := []string{"pipeline", "upload", "--format", "github-actions", "--gha-event-path", "event.json", "--gha-disable-runner-user", "ci.yml"}
	run := func(args ...string) []byte {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), executable, args...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("agent %v: %v\n%s\n%s", args, err, stdout.Bytes(), stderr.Bytes())
		}
		return stdout.Bytes()
	}
	var dryRun bytes.Buffer
	app := &cli.Command{Name: "buildkite-agent", Writer: &dryRun, ErrWriter: io.Discard}
	cfg := PipelineUploadConfig{
		Job: ghaTestJob, APIConfig: APIConfig{Endpoint: server.URL, AgentAccessToken: "test-job-token"},
		FilePaths: []string{"ci.yml"}, GHAEventPath: "event.json", GHADisableRunnerUser: true, DryRun: true,
	}
	if err := uploadGitHubActions(t.Context(), app, cfg, logger.Discard); err != nil {
		t.Fatal(err)
	}
	artifacts, contents, pipelines := store.snapshot()
	if len(artifacts) != 0 || len(pipelines) != 0 {
		t.Fatal("dry-run published artifacts or pipeline")
	}
	client, err := newGHAClient(PipelineUploadConfig{Job: ghaTestJob, APIConfig: APIConfig{Endpoint: server.URL, AgentAccessToken: "test-job-token"}}, logger.Discard, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := client.Compile(t.Context(), gha.CompileRequest{
		WorkflowPaths: []string{"ci.yml"}, EventPath: "event.json", DisableRunnerUser: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(dryRun.Bytes(), compiled.Pipeline) {
		t.Fatalf("CLI dry-run differs from typed Compile:\n%s", cmp.Diff(string(compiled.Pipeline), dryRun.String()))
	}
	cfg.DryRun = false
	if err := uploadGitHubActions(t.Context(), app, cfg, logger.Discard); err != nil {
		t.Fatal(err)
	}
	artifacts, contents, pipelines = store.snapshot()
	var expected map[string]any
	if err := yaml.Unmarshal(compiled.Pipeline, &expected); err != nil {
		t.Fatal(err)
	}
	wantJSON, err := json.Marshal(expected)
	if err != nil {
		t.Fatal(err)
	}
	gotJSON, err := json.Marshal(pipelines[0])
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(string(wantJSON), string(gotJSON)); diff != "" {
		t.Fatalf("uploaded pipeline differs from Compile (-want +got):\n%s", diff)
	}
	for _, a := range compiled.Artifacts {
		found := false
		for id, artifact := range artifacts {
			if artifact.Path == a.Path && bytes.Equal(a.Contents, contents[id]) {
				found = true
			}
		}
		if !found {
			t.Fatalf("compiled artifact %s was not uploaded intact", a.Path)
		}
	}
	if !strings.Contains(string(compiled.Pipeline), "$distribution") {
		t.Fatal("sample did not exercise generated shell interpolation")
	}
	// Compile binds plans to os.Executable, even with an explicit distribution.
	// Run the next round wholly in the built agent so it is also the compiler.
	store.mu.Lock()
	store.artifacts = make(map[string]*api.Artifact)
	store.contents = make(map[string][]byte)
	store.finished = make(map[string]bool)
	store.pipelines = nil
	store.mu.Unlock()
	builtDryRun := run(append(args, "--dry-run")...)
	artifacts, contents, pipelines = store.snapshot()
	if len(artifacts) != 0 || len(pipelines) != 0 {
		t.Fatal("built-agent dry-run published artifacts or pipeline")
	}
	run(args...)
	_, contents, pipelines = store.snapshot()
	if err := yaml.Unmarshal(builtDryRun, &expected); err != nil {
		t.Fatal(err)
	}
	wantJSON, err = json.Marshal(expected)
	if err != nil {
		t.Fatal(err)
	}
	gotJSON, err = json.Marshal(pipelines[0])
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(string(wantJSON), string(gotJSON)); diff != "" {
		t.Fatalf("built-agent upload differs from dry-run:\n%s", diff)
	}

	// Execute the actual emitted bootstrap, including download + sha256 checks,
	// not a hand-written approximation of the generated runtime arguments.
	var executionOutput bytes.Buffer
	executeSteps := func(p map[string]any, stage bool) {
		t.Helper()
		var visit func([]any)
		visit = func(steps []any) {
			for _, value := range steps {
				step := value.(map[string]any)
				if nested, ok := step["steps"].([]any); ok {
					visit(nested)
					continue
				}
				command, ok := step["command"].(string)
				if !ok {
					continue
				}
				isStage := strings.Contains(command, "upload --stage-digest")
				if isStage != stage {
					continue
				}
				key := step["key"].(string)
				store.mu.Lock()
				job := fmt.Sprintf("%08d-2222-4333-8444-555555555555", len(store.jobs)+10)
				store.jobs[key] = job
				store.mu.Unlock()
				if key == "gha-plan" {
					// Exercise the clean local-plan form on a successful job,
					// using the opaque plan identified by the emitted digest.
					_, tail, ok := strings.Cut(command, "run-job --plan-digest '")
					if !ok {
						t.Fatal("producer bootstrap has no plan digest")
					}
					digest, _, _ := strings.Cut(tail, "'")
					var plan []byte
					for _, data := range contents {
						if "sha256:"+fmt.Sprintf("%x", sha256.Sum256(data)) == digest {
							plan = data
						}
					}
					if plan == nil {
						t.Fatal("producer plan artifact missing")
					}
					path := filepath.Join(t.TempDir(), "plan.json")
					if err := os.WriteFile(path, plan, 0o600); err != nil {
						t.Fatal(err)
					}
					old := `"$distribution" run-job --plan-digest ` + shellQuote(digest) + " --plan-producer " + shellQuote(ghaTestJob)
					clean := "BUILDKITE_GHA_PLAN_DIGEST=" + shellQuote(digest) + ` "$distribution" gha run-job --plan ` + shellQuote(path) + " --artifact-producer " + shellQuote(ghaTestJob)
					if !strings.Contains(command, old) {
						t.Fatal("producer bootstrap did not match the runtime protocol")
					}
					command = strings.Replace(command, old, clean, 1)
				}
				cmd := exec.CommandContext(t.Context(), "bash", "-e", "-c", command)
				cmd.Env = append(os.Environ(), "BUILDKITE_JOB_ID="+job, "BUILDKITE_STEP_KEY="+key)
				environ, _ := step["env"].(map[string]any)
				for key, value := range environ {
					cmd.Env = append(cmd.Env, key+"="+fmt.Sprint(value))
				}
				output, err := cmd.CombinedOutput()
				executionOutput.Write(output)
				code := 0
				if err != nil {
					var exit *exec.ExitError
					if !errors.As(err, &exit) {
						t.Fatal(err)
					}
					code = exit.ExitCode()
				}
				want := 0
				if key == "gha-tolerated" {
					want = 78
				}
				if code != want {
					t.Fatalf("generated step %s: exit %d, want %d\n%s", key, code, want, output)
				}
			}
		}
		visit(p["steps"].([]any))
	}
	executeSteps(pipelines[0], false)
	executeSteps(pipelines[0], true)
	_, _, pipelines = store.snapshot()
	if len(pipelines) != 2 {
		t.Fatalf("deferred stage published %d pipelines, want 2 total", len(pipelines))
	}
	executeSteps(pipelines[1], false)
	for _, target := range []string{"first", "second"} {
		if !strings.Contains(executionOutput.String(), "matrix-target="+target+"\n") {
			t.Fatalf("missing matrix result %s:\n%s", target, executionOutput.Bytes())
		}
	}
	// The clean command must reach the same typed handler, not a missing CLI.
	output, err := exec.CommandContext(t.Context(), executable, "gha", "run-job", "--plan", "missing-plan").CombinedOutput()
	if err == nil || !bytes.Contains(output, []byte("missing-plan")) {
		t.Fatalf("clean runtime form: %v\n%s", err, output)
	}
}

type ghaTestAPI struct {
	t         *testing.T
	url       string
	mu        sync.Mutex
	artifacts map[string]*api.Artifact
	contents  map[string][]byte
	finished  map[string]bool
	jobs      map[string]string
	pipelines []map[string]any
}

func (s *ghaTestAPI) snapshot() (map[string]*api.Artifact, map[string][]byte, []map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return maps.Clone(s.artifacts), maps.Clone(s.contents), slices.Clone(s.pipelines)
}

func (s *ghaTestAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	write := func(v any) {
		s.t.Helper()
		if err := json.NewEncoder(w).Encode(v); err != nil {
			s.t.Error(err)
		}
	}
	if !strings.HasPrefix(r.URL.Path, "/storage/") && r.Header.Get("Authorization") != "Token test-job-token" {
		s.t.Error("missing job authentication")
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	switch {
	case strings.HasSuffix(r.URL.Path, "/github-actions/runners"):
		if r.Header.Get("Buildkite-Test") != "gha" {
			s.t.Error("missing server-specified header on GHA Agent API request")
		}
		var req struct {
			Requirements []struct {
				ID string `json:"id"`
			} `json:"requirements"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.t.Error(err)
		}
		resolutions := []any{}
		for _, q := range req.Requirements {
			resolutions = append(resolutions, map[string]any{"id": q.ID, "validated": true, "target": map[string]any{"queue": "gha-poc", "platform": runtime.GOOS + "/" + runtime.GOARCH, "tool_cache": false}})
		}
		write(map[string]any{"resolutions": resolutions})
	case strings.HasSuffix(r.URL.Path, "/artifacts") && r.Method == "POST":
		var batch api.ArtifactBatch
		if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
			s.t.Error(err)
		}
		ids := []string{}
		for _, a := range batch.Artifacts {
			id := fmt.Sprint(len(s.artifacts) + 1)
			a.ID, a.JobID, a.URL = id, parts[1], s.url+"/storage/"+id
			s.artifacts[id] = a
			ids = append(ids, id)
		}
		write(api.ArtifactBatchCreateResponse{ID: "batch", ArtifactIDs: ids, InstructionsTemplate: &api.ArtifactUploadInstructions{Action: api.ArtifactUploadAction{URL: s.url, Path: "/storage/upload", Method: "POST", FileInput: "file"}, Data: map[string]string{"path": "${artifact:path}"}}})
	case r.URL.Path == "/storage/upload":
		if err := r.ParseMultipartForm(8 << 20); err != nil {
			s.t.Error(err)
			return
		}
		defer r.MultipartForm.RemoveAll() //nolint:errcheck // Temporary form files.
		f, header, err := r.FormFile("file")
		if err != nil {
			s.t.Error(err)
			return
		}
		defer f.Close() //nolint:errcheck // Read-only request file.
		data, err := io.ReadAll(f)
		if err != nil {
			s.t.Error(err)
			return
		}
		for id, a := range s.artifacts {
			if a.Path == r.FormValue("path") && s.contents[id] == nil {
				if a.Sha256Sum != fmt.Sprintf("%x", sha256.Sum256(data)) {
					s.t.Errorf("artifact %s hash mismatch", header.Filename)
				}
				s.contents[id] = data
				break
			}
		}
		w.WriteHeader(200)
	case strings.HasPrefix(r.URL.Path, "/storage/"):
		_, _ = w.Write(s.contents[parts[1]])
	case strings.HasSuffix(r.URL.Path, "/artifacts") && r.Method == "PUT":
		var update api.ArtifactBatchUpdateRequest
		if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
			s.t.Error(err)
		}
		for _, a := range update.Artifacts {
			if a.State == "finished" {
				s.finished[a.ID] = true
			}
		}
		write(map[string]any{})
	case strings.HasSuffix(r.URL.Path, "/artifacts/search"):
		found := []*api.Artifact{}
		for id, a := range s.artifacts {
			scope := r.URL.Query().Get("scope")
			if a.Path == r.URL.Query().Get("query") && s.finished[id] && (scope == a.JobID || s.jobs[scope] == a.JobID) {
				found = append(found, a)
			}
		}
		write(found)
	case strings.HasSuffix(r.URL.Path, "/pipelines"):
		for id := range s.artifacts {
			if !s.finished[id] || s.contents[id] == nil {
				s.t.Errorf("pipeline preceded completed artifact %s", id)
			}
		}
		var change struct {
			Pipeline map[string]any `json:"pipeline"`
		}
		if err := json.NewDecoder(r.Body).Decode(&change); err != nil {
			s.t.Error(err)
		}
		s.pipelines = append(s.pipelines, change.Pipeline)
		write(map[string]any{})
	case strings.HasSuffix(r.URL.Path, "/annotations"), strings.HasSuffix(r.URL.Path, "/data/set"):
		write(map[string]any{})
	case strings.HasPrefix(r.URL.Path, "/steps/"):
		write(map[string]any{"output": "test job"})
	default:
		s.t.Errorf("unexpected Agent API request %s %s", r.Method, r.URL.Path)
		http.Error(w, "unexpected request", 404)
	}
}

func TestGHASecretDebugRedactionFailure(t *testing.T) {
	const secret = "sentinel-secret-must-not-be-logged"
	t.Setenv("BUILDKITE_AGENT_JOB_API_SOCKET", "")
	t.Setenv("BUILDKITE_REQUEST_HEADER_BUILDKITE_TEST", "gha")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Token test-token" || r.Header.Get("Buildkite-Test") != "gha" {
			t.Error("secret request lost authentication or server headers")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(api.Secret{Key: "example", Value: secret})
	}))
	defer server.Close()
	l := logger.NewBuffer()
	b := &ghaBackend{client: api.NewClient(l, api.Config{Endpoint: server.URL, Token: "test-token", DebugHTTP: true}), log: l, job: ghaTestJob}
	value, err := b.ResolveSecret(t.Context(), "example")
	if err == nil || value != "" {
		t.Fatal("returned secret without registering redaction")
	}
	if strings.Contains(strings.Join(l.Messages, "\n"), secret) || strings.Contains(err.Error(), secret) {
		t.Fatal("secret leaked into debug logs or error")
	}
}

func TestGHAMetadataBoundary(t *testing.T) {
	t.Parallel()
	for _, size := range []int{1022, 1023} {
		t.Run(fmt.Sprintf("value-bytes-%d", size+2), func(t *testing.T) {
			t.Parallel()
			value := strings.Repeat("a", size) + "λ"
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(api.MetaData{Value: value})
			}))
			defer server.Close()
			b := &ghaBackend{client: api.NewClient(logger.Discard, api.Config{Endpoint: server.URL, Token: "test"}), job: ghaTestJob}
			got, err := b.GetMetadataBounded(t.Context(), "buildkite:webhook", 1024)
			if size == 1022 {
				if err != nil || string(got) != value {
					t.Fatalf("value at byte limit: %q, %v", got, err)
				}
			} else if err == nil || len(got) != 0 || errors.Is(err, gha.ErrMetadataUnavailable) {
				t.Fatalf("oversized value must fail, not fall back: %q, %v", got, err)
			}
		})
	}
	for _, tc := range []struct {
		status      int
		message     string
		unavailable bool
	}{
		{400, "Build was not triggered by a webhook", true},
		{404, "Build webhook is not available", true},
		{404, "Unknown job", false},
		{401, "Build webhook is not available", false},
	} {
		t.Run(fmt.Sprint(tc.status, tc.message), func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_ = json.NewEncoder(w).Encode(map[string]string{"message": tc.message})
			}))
			defer server.Close()
			b := &ghaBackend{client: api.NewClient(logger.Discard, api.Config{Endpoint: server.URL, Token: "test"}), job: ghaTestJob}
			_, err := b.GetMetadataBounded(t.Context(), "buildkite:webhook", 1024)
			if errors.Is(err, gha.ErrMetadataUnavailable) != tc.unavailable {
				t.Fatalf("metadata error = %v", err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			if _, err := b.GetMetadataBounded(ctx, "buildkite:webhook", 1024); !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled metadata request: %v", err)
			}
		})
	}
}

func TestGHAJobIdentity(t *testing.T) {
	for key, value := range map[string]string{
		"BUILDKITE_JOB_ID": ghaTestJob, "BUILDKITE_AGENT_ACCESS_TOKEN": "test-token",
		"BUILDKITE_AGENT_ENDPOINT":  "https://agent.example.invalid/v3",
		"BUILDKITE_AGENT_JWKS_FILE": "", "BUILDKITE_AGENT_AWS_KMS_KEY": "", "BUILDKITE_AGENT_GCP_KMS_KEY": "",
	} {
		t.Setenv(key, value)
	}
	base := PipelineUploadConfig{Job: ghaTestJob, APIConfig: APIConfig{Endpoint: os.Getenv("BUILDKITE_AGENT_ENDPOINT"), AgentAccessToken: "test-token"}}
	for _, field := range []string{"job", "endpoint", "token", "signing"} {
		t.Run(field, func(t *testing.T) {
			cfg := base
			switch field {
			case "job":
				cfg.Job = "other-job"
			case "endpoint":
				cfg.Endpoint = "https://other.example.invalid/v3"
			case "token":
				cfg.AgentAccessToken = "other-token"
			case "signing":
				cfg.JWKSFile = "key.json"
			}
			_, err := newGHAClient(cfg, logger.Discard, io.Discard, io.Discard)
			if err == nil {
				t.Fatal("accepted mismatched identity or unsigned pipeline")
			}
			if strings.Contains(err.Error(), "test-token") || strings.Contains(err.Error(), "other-token") {
				t.Fatal("identity error exposed a token")
			}
		})
	}
}

func TestGHAProtocolArgs(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		args   []string
		routed bool
	}{
		{[]string{"agent", "run-job", "--plan", "plan.json"}, true},
		{[]string{"agent", "upload", "--stage-digest=sha256:abc", "--stage-producer", "job"}, true},
		{[]string{"agent", "pipeline", "upload", "--format", "github-actions", "ci.yml"}, false},
		{[]string{"agent", "--version"}, false},
		{[]string{"agent", "artifact", "upload", "--stage-digest"}, false},
	} {
		got := GHAProtocolArgs(tc.args)
		want := tc.args
		if tc.routed {
			want = append([]string{"agent", "gha"}, tc.args[1:]...)
		}
		if !slices.Equal(got, want) {
			t.Fatalf("dispatch %v = %v, want %v", tc.args, got, want)
		}
	}
}
