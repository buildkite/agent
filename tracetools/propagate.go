package tracetools

import (
	"bytes"
	"encoding/base64"
	"encoding/gob"

	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace"
	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace/tracer"
)

// EnvVarPropagator is a custom distributed tracing propagator that uses environment
// variables as the transport. This can be used to implement distributed tracing
// across shell processes where turn-key tracing solutions may not exist.
//
// See usage in the example test.
//
// Internally, this uses a text map carrier and then serializes and encodes the data
// so that it can used as an environment variable.
//
// Why the carrier data into a single string? Since carrier data is just key-value
// pairs, they could be directly added as env vars. However, when propagating the
// env vars to new processes, users will have to 1) know the list of carrier keys
// and 2) iterate and push them through. Since carrier keys are a bit of an
// implementation detail, they can change easily. So containing it all in one well
// known env var makes things simpler.
//
// TODO: fallback to BUILD_ID/JOB_ID?
type EnvVarPropagator struct {
	propagator tracer.Propagator
	EnvVarKey  string
}

func NewEnvVarPropagator(envvarKey string) *EnvVarPropagator {
	return &EnvVarPropagator{
		propagator: tracer.NewPropagator(&tracer.PropagatorConfig{}),
		EnvVarKey:  envvarKey,
	}
}

// Inject will serialize and encode tracing data into the given carrier.
// The carrier must be a map[string]string.
func (p *EnvVarPropagator) Inject(ctx ddtrace.SpanContext, carrier interface{}) error {
	switch carrier.(type) {
	case map[string]string:
	default:
		return tracer.ErrInvalidCarrier
	}

	// We use a text map carrier internally because Datadog doesn't support binary
	// carrier format (even though that's against the spec).
	textmap := tracer.TextMapCarrier{}
	if err := p.propagator.Inject(ctx, textmap); err != nil {
		return err
	}

	buf := bytes.NewBuffer([]byte{})
	enc := gob.NewEncoder(buf)
	if err := enc.Encode(textmap); err != nil {
		return err
	}

	contextString := base64.URLEncoding.EncodeToString(buf.Bytes())

	env, _ := carrier.(map[string]string)
	env[p.EnvVarKey] = contextString
	return nil
}

// Extract will decode, deserialize, and extract the tracing data from the given
// carrier. The carrier must be a map[string]string.
func (p *EnvVarPropagator) Extract(carrier interface{}) (ddtrace.SpanContext, error) {
	encodedString := ""
	switch env := carrier.(type) {
	case map[string]string:
		encodedString = env[p.EnvVarKey]
	default:
		return nil, tracer.ErrInvalidCarrier
	}

	contextBytes, err := base64.URLEncoding.DecodeString(encodedString)
	if err != nil {
		return nil, err
	}

	buf := bytes.NewBuffer(contextBytes)
	dec := gob.NewDecoder(buf)
	// We use a text map carrier internally because Datadog doesn't support binary
	// carrier format (even though that's against the spec).
	textmap := tracer.TextMapCarrier{}
	if err := dec.Decode(&textmap); err != nil {
		return nil, err
	}

	return p.propagator.Extract(textmap)
}
