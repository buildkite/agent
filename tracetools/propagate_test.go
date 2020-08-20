package tracetools

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace/tracer"
)

// nullLogger is meant to make Datadog tracing logs go nowhere during tests.
type nullLogger struct{}

func (n nullLogger) Log(_ string) {}

func TestEnvVarPropagator(t *testing.T) {
	// Need the tracer to help test some stuff, but lets configure it so nothing
	// goes out from tests and if it does, it tries to talk to an unlikely agent.
	tracer.Start(
		tracer.WithSampler(tracer.NewRateSampler(0.0)),
		tracer.WithAgentAddr("10.0.0.1:65534"),
		tracer.WithLogger(&nullLogger{}),
	)
	defer tracer.Stop()
	span := tracer.StartSpan("test")
	defer span.Finish()

	t.Run("New", func(t *testing.T) {
		e := NewEnvVarPropagator("asd")
		assert.Equal(t, "asd", e.EnvVarKey)
	})

	t.Run("Inject and extract", func(t *testing.T) {
		e := NewEnvVarPropagator("ASD")
		carrier := map[string]string{}
		err := e.Inject(span.Context(), carrier)
		assert.NoError(t, err)
		assert.Contains(t, carrier, "ASD")
		assert.NotEmpty(t, carrier["ASD"])

		sctx, err := e.Extract(carrier)
		assert.NoError(t, err)
		assert.NotEqual(t, 0, sctx.SpanID())
		assert.NotEqual(t, 0, sctx.TraceID())
	})

	t.Run("Bad carrier types", func(t *testing.T) {
		e := NewEnvVarPropagator("ASD")
		carrier := make([]string, 0, 1)

		err := e.Inject(span.Context(), carrier)
		assert.Error(t, err)
		assert.Equal(t, tracer.ErrInvalidCarrier, err)

		sctx, err := e.Extract(carrier)
		assert.Error(t, err)
		assert.Nil(t, sctx)
		assert.Equal(t, tracer.ErrInvalidCarrier, err)
	})
}
