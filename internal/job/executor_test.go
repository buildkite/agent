package job

import (
	"context"
	"testing"

	"github.com/buildkite/agent/v3/internal/shell"
	"github.com/buildkite/agent/v3/tracetools"
	"github.com/opentracing/opentracing-go"
	"github.com/stretchr/testify/assert"
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
		assert.Equal(t, test.expected, dirForAgentName(test.agentName))
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
		assert.Equal(t, test.expected, dirForRepository(test.repositoryName))
	}
}

func TestStartTracing_NoTracingBackend(t *testing.T) {
	var err error

	// When there's no tracing backend, the tracer should be a no-op.
	e := New(ExecutorConfig{})

	oriCtx := context.Background()
	e.shell, err = shell.New()
	assert.NoError(t, err)

	span, _, stopper := e.startTracing(oriCtx)
	assert.Equal(t, span, &tracetools.NoopSpan{})
	span.FinishWithError(nil) // Finish the nil span, just for completeness' sake

	// If you call opentracing.GlobalTracer() without having set it first, it returns a NoopTracer
	// In this test case, we haven't touched opentracing at all, so we get the NoopTracer
	assert.IsType(t, opentracing.NoopTracer{}, opentracing.GlobalTracer())
	stopper()
}

func TestStartTracing_Datadog(t *testing.T) {
	var err error

	// With the Datadog tracing backend, the global tracer should be from Datadog.
	cfg := ExecutorConfig{TracingBackend: "datadog"}
	e := New(cfg)

	oriCtx := context.Background()
	e.shell, err = shell.New()
	assert.NoError(t, err)

	span, ctx, stopper := e.startTracing(oriCtx)
	span.FinishWithError(nil)

	assert.IsType(t, opentracer.New(), opentracing.GlobalTracer())
	spanImpl, ok := span.(*tracetools.OpenTracingSpan)
	assert.True(t, ok)

	assert.Equal(t, spanImpl.Span, opentracing.SpanFromContext(ctx))
	stopper()
}
