package job

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/internal/shell"
	"github.com/buildkite/agent/v3/tracetools"
	"github.com/google/go-cmp/cmp"
	"github.com/opentracing/opentracing-go"
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

func TestStartTracing_NoTracingBackend(t *testing.T) {
	var err error

	// When there's no tracing backend, the tracer should be a no-op.
	e := New(ExecutorConfig{})

	oriCtx := context.Background()
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
	if got, want := reflect.TypeOf(opentracing.GlobalTracer()), reflect.TypeOf(opentracing.NoopTracer{}); got != want {
		t.Errorf("opentracing.GlobalTracer() = %v, want %v", got, want)
	}
	stopper()
}

func TestStartTracing_Datadog(t *testing.T) {
	var err error

	// With the Datadog tracing backend, the global tracer should be from Datadog.
	cfg := ExecutorConfig{TracingBackend: "datadog"}
	e := New(cfg)

	oriCtx := context.Background()
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
