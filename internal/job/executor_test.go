package job

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/internal/shell"
	"github.com/buildkite/agent/v3/tracetools"
	"github.com/google/go-cmp/cmp"
	"github.com/opentracing/opentracing-go"
	"go.opentelemetry.io/otel/trace"
	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace/opentracer"
)

var agentNameTests = []struct {
	agentName string
	expected  string
}{
	{"My Agent", "My-Agent"},
	{":docker: My Agent", "-docker--My-Agent"},
	{"My \"Agent\"", "My--Agent-"},
}

func TestDirForAgentName(t *testing.T) {
	t.Parallel()

	for _, test := range agentNameTests {
		if got, want := dirForAgentName(test.agentName), test.expected; got != want {
			t.Errorf("dirForAgentName(test.agentName) = %q, want %q", got, want)
		}
	}
}

var repositoryNameTests = []struct {
	repositoryName string
	expected       string
}{
	{"git@github.com:acme-inc/my-project.git", "git-github-com-acme-inc-my-project-git"},
	{"https://github.com/acme-inc/my-project.git", "https---github-com-acme-inc-my-project-git"},
}

func TestDirForRepository(t *testing.T) {
	t.Parallel()

	for _, test := range repositoryNameTests {
		if got, want := dirForRepository(test.repositoryName), test.expected; got != want {
			t.Errorf("dirForRepository(test.repositoryName) = %q, want %q", got, want)
		}
	}
}

type errorWriter struct{ err error }

func (w errorWriter) Write([]byte) (int, error) { return 0, w.err }

func TestTeeWriterReturnsSecondaryWriteError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("secondary write failed")
	var primary bytes.Buffer
	w := &teeWriter{
		primary:   &primary,
		secondary: errorWriter{err: wantErr},
	}
	data := []byte("control output")

	n, err := w.Write(data)
	if !errors.Is(err, wantErr) {
		t.Errorf("w.Write() error = %v, want %v", err, wantErr)
	}
	if n != len(data) {
		t.Errorf("w.Write() wrote %d bytes to the primary, want %d", n, len(data))
	}
	if got, want := primary.String(), string(data); got != want {
		t.Errorf("primary output = %q, want %q", got, want)
	}
}

func TestStartTracing_NoTracingBackend(t *testing.T) {
	var err error

	// When there's no tracing backend, the tracer should be a no-op.
	e := New(ExecutorConfig{})

	oriCtx := t.Context()
	e.shell, err = shell.New()
	if err != nil {
		t.Errorf("shell.New() error = %v, want nil", err)
	}

	span, _, stopper := e.startTracing(oriCtx)
	if diff := cmp.Diff(span, &tracetools.NoopSpan{}); diff != "" {
		t.Errorf("e.startTracing(oriCtx) diff (-got +want):\n%s", diff)
	}
	span.FinishWithError(nil) // Finish the nil span, just for completeness' sake

	// If you call opentracing.GlobalTracer() without having set it first, it returns a NoopTracer
	// In this test case, we haven't touched opentracing at all, so we get the NoopTracer
	if got, want := reflect.TypeOf(opentracing.GlobalTracer()), reflect.TypeFor[opentracing.NoopTracer](); got != want {
		t.Errorf("opentracing.GlobalTracer() = %v, want %v", got, want)
	}
	stopper()
}

func TestStartTracing_Datadog(t *testing.T) {
	var err error

	// With the Datadog tracing backend, the global tracer should be from Datadog.
	cfg := ExecutorConfig{TracingBackend: "datadog"}
	e := New(cfg)

	oriCtx := t.Context()
	e.shell, err = shell.New()
	if err != nil {
		t.Errorf("shell.New() error = %v, want nil", err)
	}

	span, ctx, stopper := e.startTracing(oriCtx)
	span.FinishWithError(nil)

	if got, want := reflect.TypeOf(opentracing.GlobalTracer()), reflect.TypeOf(opentracer.New()); got != want {
		t.Errorf("opentracing.GlobalTracer() = %v, want %v", got, want)
	}
	spanImpl, ok := span.(*tracetools.OpenTracingSpan)
	if got := ok; !got {
		t.Errorf("span.(*tracetools.OpenTracingSpan) = %t, want true", got)
	}

	if got, want := opentracing.SpanFromContext(ctx), spanImpl.Span; !reflect.DeepEqual(got, want) {
		t.Errorf("opentracing.SpanFromContext(ctx) = %v, want %v", got, want)
	}
	stopper()
}

func TestContextWithTraceparentIfEnabledDoesNotAcceptServerTraceparentWithoutPropagation(t *testing.T) {
	t.Parallel()

	sh, err := shell.New(shell.WithLogger(shell.DiscardLogger))
	if err != nil {
		t.Fatalf("shell.New() error = %v", err)
	}
	e := New(ExecutorConfig{
		TracingTraceParent: "00-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-bbbbbbbbbbbbbbbb-01",
	})
	e.shell = sh

	ctx := e.contextWithTraceparentIfEnabled(t.Context())
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		t.Fatalf("SpanContextFromContext(ctx).IsValid() = true, want false")
	}
}

func TestContextWithTraceparentIfEnabledAcceptsServerTraceparentWithPropagation(t *testing.T) {
	t.Parallel()

	sh, err := shell.New(shell.WithLogger(shell.DiscardLogger))
	if err != nil {
		t.Fatalf("shell.New() error = %v", err)
	}
	e := New(ExecutorConfig{
		TracingPropagateTraceparent: true,
		TracingTraceParent:          "00-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-bbbbbbbbbbbbbbbb-01",
	})
	e.shell = sh

	ctx := e.contextWithTraceparentIfEnabled(t.Context())
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		t.Fatalf("SpanContextFromContext(ctx).IsValid() = false, want true")
	}
	if got, want := sc.TraceID().String(), "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"; got != want {
		t.Fatalf("SpanContextFromContext(ctx).TraceID() = %q, want %q", got, want)
	}
}

// newCancelTestExecutor returns an Executor whose shell.Env starts empty,
// suitable for exercising Cancel without depending on the host environment.
func newCancelTestExecutor(t *testing.T) *Executor {
	t.Helper()

	e := New(ExecutorConfig{})

	sh, err := shell.New(shell.WithEnv(env.New()))
	if err != nil {
		t.Fatalf("shell.New() error = %v, want nil", err)
	}
	e.shell = sh

	return e
}

// TestSetUpGitLFSSkipSmudge is a regression test for #4041: setUp used to set
// GIT_LFS_SKIP_SMUDGE=1 unconditionally, disabling git's default LFS smudge for
// every job. It must only be set when LFS handling is enabled.
func TestSetUpGitLFSSkipSmudge(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name       string
		lfsEnabled bool
		wantSkip   bool
	}{
		{name: "lfs disabled leaves smudge enabled", lfsEnabled: false, wantSkip: false},
		{name: "lfs enabled skips smudge", lfsEnabled: true, wantSkip: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			e := New(ExecutorConfig{GitLFSEnabled: test.lfsEnabled})

			sh, err := shell.New(shell.WithEnv(env.New()))
			if err != nil {
				t.Fatalf("shell.New() error = %v, want nil", err)
			}
			// setUp requires a checkout path; supply one so it doesn't error.
			sh.Env.Set("BUILDKITE_BUILD_CHECKOUT_PATH", t.TempDir())
			e.shell = sh

			if err := e.setUp(t.Context()); err != nil {
				t.Fatalf("e.setUp() error = %v, want nil", err)
			}

			got, ok := e.shell.Env.Get("GIT_LFS_SKIP_SMUDGE")
			if ok != test.wantSkip {
				t.Errorf("GIT_LFS_SKIP_SMUDGE present = %v, want %v", ok, test.wantSkip)
			}
			if test.wantSkip && got != "1" {
				t.Errorf("GIT_LFS_SKIP_SMUDGE = %q, want %q", got, "1")
			}
		})
	}
}

// TestCancelSetsJobCancelledEnv verifies the precedent set in #3213: any
// cancellation surfaces BUILDKITE_JOB_CANCELLED=true to the post-command hook.
func TestCancelSetsJobCancelledEnv(t *testing.T) {
	t.Parallel()

	e := newCancelTestExecutor(t)

	if err := e.Cancel(); err != nil {
		t.Fatalf("e.Cancel() = %v, want nil", err)
	}

	if got, ok := e.shell.Env.Get("BUILDKITE_JOB_CANCELLED"); !ok || got != "true" {
		t.Errorf(`e.shell.Env.Get("BUILDKITE_JOB_CANCELLED") = (%q, %v), want ("true", true)`, got, ok)
	}
	if _, ok := e.shell.Env.Get("BUILDKITE_JOB_TIMED_OUT"); ok {
		t.Errorf("BUILDKITE_JOB_TIMED_OUT was set on a non-timeout cancellation, want unset")
	}
}

// TestCancelSetsJobTimedOutEnvWhenMarkerExists verifies that when the agent
// drops the timeout marker file before signaling, Cancel surfaces
// BUILDKITE_JOB_TIMED_OUT=true alongside BUILDKITE_JOB_CANCELLED.
func TestCancelSetsJobTimedOutEnvWhenMarkerExists(t *testing.T) {
	t.Parallel()

	e := newCancelTestExecutor(t)

	markerPath := filepath.Join(t.TempDir(), "job-timeout-marker")
	if err := os.WriteFile(markerPath, []byte("true"), 0o644); err != nil {
		t.Fatalf("os.WriteFile(%q) = %v", markerPath, err)
	}
	e.shell.Env.Set("BUILDKITE_AGENT_JOB_TIMEOUT_FILE", markerPath)

	if err := e.Cancel(); err != nil {
		t.Fatalf("e.Cancel() = %v, want nil", err)
	}

	if got, ok := e.shell.Env.Get("BUILDKITE_JOB_CANCELLED"); !ok || got != "true" {
		t.Errorf(`e.shell.Env.Get("BUILDKITE_JOB_CANCELLED") = (%q, %v), want ("true", true)`, got, ok)
	}
	if got, ok := e.shell.Env.Get("BUILDKITE_JOB_TIMED_OUT"); !ok || got != "true" {
		t.Errorf(`e.shell.Env.Get("BUILDKITE_JOB_TIMED_OUT") = (%q, %v), want ("true", true)`, got, ok)
	}
}

// TestCancelDoesNotSetTimedOutWhenMarkerMissing verifies that having the env
// var pointing at a path that does not exist (the normal case for a non-
// timeout cancellation) does not falsely flag the job as timed out.
func TestCancelDoesNotSetTimedOutWhenMarkerMissing(t *testing.T) {
	t.Parallel()

	e := newCancelTestExecutor(t)

	missingPath := filepath.Join(t.TempDir(), "does-not-exist")
	e.shell.Env.Set("BUILDKITE_AGENT_JOB_TIMEOUT_FILE", missingPath)

	if err := e.Cancel(); err != nil {
		t.Fatalf("e.Cancel() = %v, want nil", err)
	}

	if got, ok := e.shell.Env.Get("BUILDKITE_JOB_CANCELLED"); !ok || got != "true" {
		t.Errorf(`e.shell.Env.Get("BUILDKITE_JOB_CANCELLED") = (%q, %v), want ("true", true)`, got, ok)
	}
	if _, ok := e.shell.Env.Get("BUILDKITE_JOB_TIMED_OUT"); ok {
		t.Errorf("BUILDKITE_JOB_TIMED_OUT was set despite missing marker file, want unset")
	}
}

func TestUseRepositoryProviderGitCredentials(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want bool
	}{
		{name: "generic flag", env: map[string]string{"BUILDKITE_USE_REPOSITORY_PROVIDER_GIT_CREDENTIALS": "true"}, want: true},
		{name: "legacy flag", env: map[string]string{"BUILDKITE_USE_GITHUB_APP_GIT_CREDENTIALS": "true"}, want: true},
		{name: "both flags", env: map[string]string{
			"BUILDKITE_USE_REPOSITORY_PROVIDER_GIT_CREDENTIALS": "true",
			"BUILDKITE_USE_GITHUB_APP_GIT_CREDENTIALS":          "true",
		}, want: true},
		{name: "generic false", env: map[string]string{"BUILDKITE_USE_REPOSITORY_PROVIDER_GIT_CREDENTIALS": "false"}},
		{name: "unset"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			e := New(ExecutorConfig{})
			sh, err := shell.New(shell.WithEnv(env.New()))
			if err != nil {
				t.Fatal(err)
			}
			e.shell = sh
			for key, value := range test.env {
				sh.Env.Set(key, value)
			}
			if got := e.useRepositoryProviderGitCredentials(); got != test.want {
				t.Errorf("useRepositoryProviderGitCredentials() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestConfigureRepositoryProviderGitCredentials(t *testing.T) {
	tests := []struct {
		name              string
		repository        string
		env               map[string]string
		wantSSHKeyscan    bool
		wantSkipRepoScan  bool
		wantGitHubRewrite bool
	}{
		{
			name:              "GitHub SSH primary rewrites under provider flag",
			repository:        "git@github.com:acme/widgets.git",
			env:               map[string]string{"BUILDKITE_USE_REPOSITORY_PROVIDER_GIT_CREDENTIALS": "true"},
			wantSSHKeyscan:    true,
			wantSkipRepoScan:  true,
			wantGitHubRewrite: true,
		},
		{
			name:           "provider HTTPS does not rewrite unrelated GitHub SSH remotes",
			repository:     "https://git.example.com/acme/widgets.git",
			env:            map[string]string{"BUILDKITE_USE_REPOSITORY_PROVIDER_GIT_CREDENTIALS": "true"},
			wantSSHKeyscan: true,
		},
		{
			name:           "provider non-GitHub SSH keeps keyscan without rewrite",
			repository:     "git@git.example.com:acme/widgets.git",
			env:            map[string]string{"BUILDKITE_USE_REPOSITORY_PROVIDER_GIT_CREDENTIALS": "true"},
			wantSSHKeyscan: true,
		},
		{
			name:              "legacy flag always rewrites GitHub SSH remotes and disables keyscan",
			repository:        "https://git.example.com/acme/widgets.git",
			env:               map[string]string{"BUILDKITE_USE_GITHUB_APP_GIT_CREDENTIALS": "true"},
			wantGitHubRewrite: true,
		},
		{
			name:       "both flags preserve legacy behavior before setUp",
			repository: "https://git.example.com/acme/widgets.git",
			env: map[string]string{
				"BUILDKITE_USE_REPOSITORY_PROVIDER_GIT_CREDENTIALS": "true",
				"BUILDKITE_USE_GITHUB_APP_GIT_CREDENTIALS":          "true",
			},
			wantGitHubRewrite: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			home := t.TempDir()
			shEnv := env.New()
			shEnv.Set("HOME", home)
			shEnv.Set("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig"))
			shEnv.Set("GIT_CONFIG_NOSYSTEM", "1")
			shEnv.Set("PATH", os.Getenv("PATH"))
			for key, value := range test.env {
				shEnv.Set(key, value)
			}
			sh, err := shell.New(shell.WithEnv(shEnv), shell.WithLogger(shell.DiscardLogger))
			if err != nil {
				t.Fatal(err)
			}
			e := New(ExecutorConfig{
				Repository: test.repository,
				SSHKeyscan: true,
			})
			e.shell = sh

			if err := e.configureRepositoryProviderGitCredentials(t.Context(), false); err != nil {
				t.Fatal(err)
			}
			if err := e.configureRepositoryProviderGitCredentials(t.Context(), true); err != nil {
				t.Fatal(err)
			}
			if got := e.SSHKeyscan; got != test.wantSSHKeyscan {
				t.Errorf("SSHKeyscan = %t, want %t", got, test.wantSSHKeyscan)
			}
			if got := e.skipRepositorySSHKeyscan; got != test.wantSkipRepoScan {
				t.Errorf("skipRepositorySSHKeyscan = %t, want %t", got, test.wantSkipRepoScan)
			}

			config, err := sh.Command("git", "config", "--global", "--list").RunAndCaptureStdout(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(config, "credential.usehttppath=true") ||
				!strings.Contains(config, "credential.helper=") {
				t.Errorf("global Git config does not contain credential helper settings:\n%s", config)
			}
			hasRewrite := strings.Contains(config, "url.https://github.com/.insteadof=git@github.com:")
			if hasRewrite != test.wantGitHubRewrite {
				t.Errorf("GitHub rewrite present = %t, want %t\n%s", hasRewrite, test.wantGitHubRewrite, config)
			}
		})
	}
}
