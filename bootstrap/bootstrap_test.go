package bootstrap

import (
	"context"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"testing"

	"github.com/buildkite/agent/v3/bootstrap/shell"
	"github.com/buildkite/agent/v3/redaction"
	"github.com/stretchr/testify/assert"
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

func TestGetValuesToRedact(t *testing.T) {
	t.Parallel()

	redactConfig := []string{
		"*_PASSWORD",
		"*_TOKEN",
	}
	environment := map[string]string{
		"BUILDKITE_PIPELINE": "unit-test",
		"DATABASE_USERNAME":  "AzureDiamond",
		"DATABASE_PASSWORD":  "hunter2",
	}

	valuesToRedact := redaction.GetValuesToRedact(shell.DiscardLogger, redactConfig, environment)

	assert.Equal(t, []string{"hunter2"}, valuesToRedact)
}

func TestGetValuesToRedactEmpty(t *testing.T) {
	t.Parallel()

	redactConfig := []string{}
	environment := map[string]string{
		"FOO":                "BAR",
		"BUILDKITE_PIPELINE": "unit-test",
	}

	valuesToRedact := redaction.GetValuesToRedact(shell.DiscardLogger, redactConfig, environment)

	var expected []string
	assert.Equal(t, expected, valuesToRedact)
	assert.Equal(t, 0, len(valuesToRedact))
}

func TestStartTracing(t *testing.T) {
	oriCtx := context.Background()
	var err error

	// When there's no Datadog tracing address, the tracer should be a no-op.
	b := New(Config{})
	b.shell, err = shell.NewWithContext(oriCtx)
	if err != nil {
		assert.FailNow(t, "Unexpected error while createing shell: %v", err)
	}
	span, ctx, stopper := b.startTracing(oriCtx)
	assert.IsType(t, trace.NewNoopTracerProvider(), otel.GetTracerProvider())
	span.End()
	stopper()
	assert.Equal(t, span, trace.SpanFromContext(ctx))
}

func TestStartTracingDatadog(t *testing.T) {
	oriCtx := context.Background()
	var err error

	// With the Datadog tracing backend, the global tracer should be from Datadog.
	cfg := Config{
		TracingBackend: "datadog",
	}
	b := New(cfg)
	b.shell, err = shell.NewWithContext(oriCtx)
	if err != nil {
		assert.FailNow(t, "Unexpected error while createing shell: %v", err)
	}
	span, ctx, stopper := b.startTracing(oriCtx)
	tracerProvider := sdktrace.TracerProvider{}
	assert.IsType(t, &tracerProvider, otel.GetTracerProvider())
	span.End()
	stopper()
	assert.Equal(t, span, trace.SpanFromContext(ctx))
}
