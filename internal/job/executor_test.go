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

func TestReadTracingConfigFromEnv(t *testing.T) {
	t.Parallel()

	t.Run("overrides backend from env", func(t *testing.T) {
		t.Parallel()
		e := New(ExecutorConfig{TracingBackend: ""})
		var err error
		e.shell, err = shell.New()
		assert.NoError(t, err)

		e.shell.Env.Set("BUILDKITE_TRACING_BACKEND", "opentelemetry")
		e.readTracingConfigFromEnv()
		assert.Equal(t, "opentelemetry", e.TracingBackend)
	})

	t.Run("disables tracing when env var unset", func(t *testing.T) {
		t.Parallel()
		e := New(ExecutorConfig{TracingBackend: "opentelemetry"})
		var err error
		e.shell, err = shell.New()
		assert.NoError(t, err)

		// Don't set BUILDKITE_TRACING_BACKEND in the env at all
		e.readTracingConfigFromEnv()
		assert.Equal(t, tracetools.BackendNone, e.TracingBackend)
	})

	t.Run("overrides propagate traceparent from env", func(t *testing.T) {
		t.Parallel()
		e := New(ExecutorConfig{TracingPropagateTraceparent: false})
		var err error
		e.shell, err = shell.New()
		assert.NoError(t, err)

		e.shell.Env.Set("BUILDKITE_TRACING_PROPAGATE_TRACEPARENT", "true")
		e.readTracingConfigFromEnv()
		assert.True(t, e.TracingPropagateTraceparent)
	})

	t.Run("overrides service name from env", func(t *testing.T) {
		t.Parallel()
		e := New(ExecutorConfig{TracingServiceName: "original"})
		var err error
		e.shell, err = shell.New()
		assert.NoError(t, err)

		e.shell.Env.Set("BUILDKITE_TRACING_SERVICE_NAME", "custom-service")
		e.readTracingConfigFromEnv()
		assert.Equal(t, "custom-service", e.TracingServiceName)
	})

	t.Run("preserves config when env matches", func(t *testing.T) {
		t.Parallel()
		e := New(ExecutorConfig{
			TracingBackend:     "opentelemetry",
			TracingServiceName: "buildkite-agent",
		})
		var err error
		e.shell, err = shell.New()
		assert.NoError(t, err)

		e.shell.Env.Set("BUILDKITE_TRACING_BACKEND", "opentelemetry")
		e.shell.Env.Set("BUILDKITE_TRACING_SERVICE_NAME", "buildkite-agent")
		e.readTracingConfigFromEnv()
		assert.Equal(t, "opentelemetry", e.TracingBackend)
		assert.Equal(t, "buildkite-agent", e.TracingServiceName)
	})
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
