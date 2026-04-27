package job

import (
	"context"
	"reflect"
	"testing"

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
		if got, want := test.expected, dirForAgentName(test.agentName); got != want {
			t.Errorf("test.expected = %q, want %q", got, want)
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
		if got, want := test.expected, dirForRepository(test.repositoryName); got != want {
			t.Errorf("test.expected = %q, want %q", got, want)
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
		t.Errorf("err error = %v, want nil", err)
	}

	span, _, stopper := e.startTracing(oriCtx)
	if diff := cmp.Diff(span, &tracetools.NoopSpan{}); diff != "" {
		t.Errorf("span diff (-got +want):\n%s", diff)
	}
	span.FinishWithError(nil) // Finish the nil span, just for completeness' sake

	// If you call opentracing.GlobalTracer() without having set it first, it returns a NoopTracer
	// In this test case, we haven't touched opentracing at all, so we get the NoopTracer'
	got := opentracing.GlobalTracer()
	if _, is := got.(opentracing.NoopTracer); !is {
		t.Errorf("opentracing.GlobalTracer() = %T, want opentracing.NoopTracer", got)
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
		t.Errorf("err error = %v, want nil", err)
	}

	span, ctx, stopper := e.startTracing(oriCtx)
	span.FinishWithError(nil)

	if got, want := reflect.TypeOf(opentracing.GlobalTracer()), reflect.TypeOf(opentracer.New()); got != want {
		t.Errorf("opentracing.GlobalTracer() = %v, want %v", got, want)
	}
	spanImpl, ok := span.(*tracetools.OpenTracingSpan)
	if got := ok; !got {
		t.Errorf("ok = %t, want true", got)
	}

	if got, want := spanImpl.Span, opentracing.SpanFromContext(ctx); !reflect.DeepEqual(got, want) {
		t.Errorf("spanImpl.Span = %v, want %v", got, want)
	}
	stopper()
}
