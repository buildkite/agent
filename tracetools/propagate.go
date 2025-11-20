package tracetools

import (
	"bytes"
	"encoding/base64"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"io"

	"github.com/opentracing/opentracing-go"
	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace/tracer"
)

// EnvVarTraceContextKey is the env var key that will be used to store/retrieve the
// encoded trace context information into env var maps.
const EnvVarTraceContextKey = "BUILDKITE_TRACE_CONTEXT"

// EncodeTraceContext will serialize and encode tracing data into a string and place
// it into the given env vars map.
func EncodeTraceContext(span opentracing.Span, env map[string]string, codec Codec) error {
	textmap := tracer.TextMapCarrier{}
	if err := span.Tracer().Inject(span.Context(), opentracing.TextMap, &textmap); err != nil {
		return err
	}

	buf := bytes.NewBuffer(nil)
	enc := codec.NewEncoder(buf)
	if err := enc.Encode(textmap); err != nil {
		return err
	}

	env[EnvVarTraceContextKey] = base64.URLEncoding.EncodeToString(buf.Bytes())
	return nil
}

// DecodeTraceContext will decode, deserialize, and extract the tracing data from the
// given env var map.
func DecodeTraceContext(env map[string]string, codec Codec) (opentracing.SpanContext, error) {
	s, has := env[EnvVarTraceContextKey]
	if !has {
		return nil, opentracing.ErrSpanContextNotFound
	}

	contextBytes, err := base64.URLEncoding.DecodeString(s)
	if err != nil {
		return nil, err
	}

	dec := codec.NewDecoder(bytes.NewReader(contextBytes))
	textmap := opentracing.TextMapCarrier{}
	if err := dec.Decode(&textmap); err != nil {
		return nil, err
	}

	return opentracing.GlobalTracer().Extract(opentracing.TextMap, textmap)
}

// Encoder impls can encode values. Decoder impls can decode values.
type (
	Encoder interface{ Encode(v any) error }
	Decoder interface{ Decode(v any) error }
)

// Codec implementations produce encoders/decoders.
type Codec interface {
	NewEncoder(io.Writer) Encoder
	NewDecoder(io.Reader) Decoder
	String() string
}

// CodecGob marshals and unmarshals with https://pkg.go.dev/encoding/gob.
type CodecGob struct{}

func (CodecGob) NewEncoder(w io.Writer) Encoder { return gob.NewEncoder(w) }
func (CodecGob) NewDecoder(r io.Reader) Decoder { return gob.NewDecoder(r) }
func (CodecGob) String() string                 { return "gob" }

// CodecJSON marshals and unmarshals with https://pkg.go.dev/encoding/json.
type CodecJSON struct{}

func (CodecJSON) NewEncoder(w io.Writer) Encoder { return json.NewEncoder(w) }
func (CodecJSON) NewDecoder(r io.Reader) Decoder { return json.NewDecoder(r) }
func (CodecJSON) String() string                 { return "json" }

// ParseEncoding converts an encoding to the associated codec.
// An empty string is parsed as "gob".
func ParseEncoding(encoding string) (Codec, error) {
	switch encoding {
	case "", "gob":
		return CodecGob{}, nil
	case "json":
		return CodecJSON{}, nil
	default:
		return nil, fmt.Errorf("invalid encoding %q", encoding)
	}
}
