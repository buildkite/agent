package tracetools

import (
	"bytes"
	"encoding/base64"
	"encoding/gob"
	"testing"

	"github.com/opentracing/opentracing-go"
	"github.com/stretchr/testify/assert"
)

// nullLogger is meant to make Datadog tracing logs go nowhere during tests.
type nullLogger struct{}

func (n nullLogger) Log(_ string) {}

func TestDecodeTraceContext(t *testing.T) {
	t.Run("No context info", func(t *testing.T) {
		sctx, err := DecodeTraceContext(map[string]string{})
		assert.Nil(t, sctx)
		assert.Equal(t, opentracing.ErrSpanContextNotFound, err)
	})

	t.Run("Invalid bsae64 context string", func(t *testing.T) {
		sctx, err := DecodeTraceContext(map[string]string{
			EnvVarTraceContextKey: "asd",
		})
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
		})
		assert.Nil(t, sctx)
		assert.Error(t, err)
	})
}
