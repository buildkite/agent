package tracetools

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/mocktracer"
	"github.com/stretchr/testify/assert"
)

// nullLogger is meant to make Datadog tracing logs go nowhere during tests.
type nullLogger struct{}

func (n nullLogger) Log(_ string) {}

func stubGlobalTracer() func() {
	oriTracer := opentracing.GlobalTracer()
	opentracing.SetGlobalTracer(mocktracer.New())
	return func() {
		opentracing.SetGlobalTracer(oriTracer)
	}
}

func TestEncodeTraceContext(t *testing.T) {
	cleanup := stubGlobalTracer()
	defer cleanup()

	testCases := []struct {
		useJson  bool
		name     string
		decoder  func(*bytes.Buffer, *opentracing.TextMapCarrier) error
		expected opentracing.TextMapCarrier
	}{
		{
			true,
			"with json encoder",
			func(b *bytes.Buffer, m *opentracing.TextMapCarrier) error { return json.NewDecoder(b).Decode(m) },
			opentracing.TextMapCarrier{"mockpfx-ids-sampled": "true", "mockpfx-ids-spanid": "46", "mockpfx-ids-traceid": "43"},
		},
		{
			false,
			"with gob encoder",
			func(b *bytes.Buffer, m *opentracing.TextMapCarrier) error { return gob.NewDecoder(b).Decode(m) },
			opentracing.TextMapCarrier{"mockpfx-ids-sampled": "true", "mockpfx-ids-spanid": "50", "mockpfx-ids-traceid": "47"},
		},
	}
	for _, test := range testCases {
		t.Run(fmt.Sprintf("Encodes %s", test.name), func(t *testing.T) {
			ctx := context.Background()
			parent := opentracing.StartSpan("job.parent")
			ctx = opentracing.ContextWithSpan(ctx, parent)

			span, ctx := opentracing.StartSpanFromContext(ctx, "job.run")
			env := map[string]string{}
			err := EncodeTraceContext(span, env, test.useJson)
			if err != nil {
				assert.FailNow(t, "unexpected encode error: %v", err)
			}
			assert.Contains(t, env, EnvVarTraceContextKey)
			assert.NotNil(t, env[EnvVarTraceContextKey])

			contextBytes, err := base64.URLEncoding.DecodeString(env[EnvVarTraceContextKey])
			if err != nil {
				assert.FailNow(t, "unexpected base64 decode error: %v", err)
			}

			buf := bytes.NewBuffer(contextBytes)
			textmap := opentracing.TextMapCarrier{}
			if err := test.decoder(buf, &textmap); err != nil {
				assert.FailNow(t, "unexpected encode error: %v", err)
			}
			// The content of the trace context will vary based on the tracer used. Not sure if hardcoding the stuff
			// from the MockTracer would be stable.
			assert.NotEmpty(t, textmap)
			assert.Equal(t, test.expected, textmap)
		})
	}
}

func TestDecodeTraceContext(t *testing.T) {
	cleanup := stubGlobalTracer()
	defer cleanup()

	t.Run("No context info", func(t *testing.T) {
		sctx, err := DecodeTraceContext(map[string]string{}, true)
		assert.Nil(t, sctx)
		assert.Equal(t, opentracing.ErrSpanContextNotFound, err)
	})

	t.Run("Invalid base64 context string", func(t *testing.T) {
		sctx, err := DecodeTraceContext(map[string]string{
			EnvVarTraceContextKey: "asd",
		}, true)
		assert.Nil(t, sctx)
		assert.Equal(t, base64.CorruptInputError(0), err)
	})

	t.Run("Invalid context data", func(t *testing.T) {
		buf := bytes.NewBuffer([]byte{})
		err := gob.NewEncoder(buf).Encode("asd")
		if err != nil {
			assert.FailNow(t, "unexpected encode error: %v", err)
		}
		s := base64.URLEncoding.EncodeToString(buf.Bytes())
		sctx, err := DecodeTraceContext(map[string]string{
			EnvVarTraceContextKey: s,
		}, true)
		assert.Nil(t, sctx)
		assert.Error(t, err)
	})

	testCases := []struct {
		useJson bool
		name    string
	}{
		{true, "with json encoder"},
		{false, "with gob encoder"},
	}
	for _, test := range testCases {
		t.Run(fmt.Sprintf("Encode-decode flows %s", test.name), func(t *testing.T) {
			span := opentracing.StartSpan("job.run")
			env := map[string]string{}
			err := EncodeTraceContext(span, env, test.useJson)
			if err != nil {
				assert.FailNow(t, "unexpected encode error: %v", err)
			}

			sctx, err := DecodeTraceContext(env, test.useJson)
			if err != nil {
				assert.FailNow(t, "unexpected decode error: %v", err)
			}
			assert.NotNil(t, sctx)
		})
	}
}
