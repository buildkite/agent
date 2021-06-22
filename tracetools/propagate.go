package tracetools

import (
	"bytes"
	"encoding/base64"
	"encoding/gob"
	"net/http"

	ot "github.com/opentracing/opentracing-go"
)

// EnvVarTraceContextKey is the env var key that will be used to store/retrieve the
// encoded trace context information into env var maps.
const EnvVarTraceContextKey = "BUILDKITE_TRACE_CONTEXT"

// EncodeTraceContext will serialize and encode tracing data into a string and place
// it into the given env vars map.
func EncodeTraceContext(span ot.Span, env map[string]string) error {
	headers := http.Header{}
	carrier := ot.HTTPHeadersCarrier(headers)
	if err := span.Tracer().Inject(span.Context(), ot.HTTPHeaders, carrier); err != nil {
		return err
	}

	buf := bytes.NewBuffer([]byte{})
	enc := gob.NewEncoder(buf)
	if err := enc.Encode(headers); err != nil {
		return err
	}

	env[EnvVarTraceContextKey] = base64.URLEncoding.EncodeToString(buf.Bytes())
	return nil
}

// DecodeTraceContext will decode, deserialize, and extract the tracing data from the
// given env var map.
func DecodeTraceContext(env map[string]string) (ot.SpanContext, error) {
	s, has := env[EnvVarTraceContextKey]
	if !has {
		return nil, ot.ErrSpanContextNotFound
	}

	contextBytes, err := base64.URLEncoding.DecodeString(s)
	if err != nil {
		return nil, err
	}

	buf := bytes.NewBuffer(contextBytes)
	dec := gob.NewDecoder(buf)
	httpheader := ot.HTTPHeadersCarrier{}
	if err := dec.Decode(&httpheader); err != nil {
		return nil, err
	}

	return ot.GlobalTracer().Extract(ot.HTTPHeaders, httpheader)
}
