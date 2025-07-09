package tracetools

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/gob"
	"errors"
	"maps"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/mocktracer"
)

func stubGlobalTracer() func() {
	oriTracer := opentracing.GlobalTracer()
	opentracing.SetGlobalTracer(mocktracer.New())
	return func() {
		opentracing.SetGlobalTracer(oriTracer)
	}
}

func TestDecodeTraceContext(t *testing.T) {
	t.Cleanup(stubGlobalTracer())

	t.Run("No context info", func(t *testing.T) {
		sctx, err := DecodeTraceContext(map[string]string{}, CodecGob{})
		if sctx != nil {
			t.Errorf("DecodeTraceContext({}, gob) = %v, want nil", sctx)
		}
		if want := opentracing.ErrSpanContextNotFound; !errors.Is(err, want) {
			t.Errorf("DecodeTraceContext({}, gob) error = %v, want %v", err, want)
		}
	})

	t.Run("Invalid bsae64 context string", func(t *testing.T) {
		sctx, err := DecodeTraceContext(map[string]string{
			EnvVarTraceContextKey: "asd",
		}, CodecGob{})
		if sctx != nil {
			t.Errorf("DecodeTraceContext({}, gob) = %v, want nil", sctx)
		}
		if want := base64.CorruptInputError(0); !errors.Is(err, want) {
			t.Errorf("DecodeTraceContext({}, gob) error = %v, want %v", err, want)
		}
	})

	t.Run("Invalid context data", func(t *testing.T) {
		buf := bytes.NewBuffer([]byte{})
		err := gob.NewEncoder(buf).Encode("asd")
		if err != nil {
			t.Fatalf("gob.NewEncoder(buf).Encode(\"asd\") error = %v", err)
		}
		s := base64.URLEncoding.EncodeToString(buf.Bytes())
		input := map[string]string{EnvVarTraceContextKey: s}
		sctx, err := DecodeTraceContext(input, CodecGob{})
		if sctx != nil {
			t.Errorf("DecodeTraceContext(%v, gob) = %v, want nil", input, sctx)
		}
		if err == nil { // gob returns string errors, not typed errors...
			t.Errorf("DecodeTraceContext(%v, gob) error = %v, want gob decoding error", input, err)
		}
	})

	for _, encoding := range []string{"", "gob", "json"} {
		t.Run(encoding, func(t *testing.T) {
			codec, err := ParseEncoding(encoding)
			if err != nil {
				t.Fatalf("ParseEncoding(%q) error = %v", encoding, err)
			}

			span := opentracing.StartSpan("job.run")
			env := map[string]string{}
			if err := EncodeTraceContext(span, env, codec); err != nil {
				t.Fatalf("EncodeTraceContext(span, %v, %v) error = %v", env, codec, err)
			}

			sctx, err := DecodeTraceContext(env, codec)
			if err != nil {
				t.Fatalf("DecodeTraceContext(%v, %v) error = %v", env, codec, err)
			}
			if sctx == nil {
				t.Errorf("DecodeTraceContext(%v, %v) = %v, want non-nil span context", env, codec, sctx)
			}
		})
	}
}

func TestEncodeTraceContext(t *testing.T) {
	t.Cleanup(stubGlobalTracer())

	testCases := []struct {
		encoding string
		want     opentracing.TextMapCarrier
	}{
		{
			encoding: "json",
			want:     opentracing.TextMapCarrier{"mockpfx-ids-sampled": "true", "mockpfx-ids-spanid": "46", "mockpfx-ids-traceid": "43"},
		},
		{
			encoding: "gob",
			want:     opentracing.TextMapCarrier{"mockpfx-ids-sampled": "true", "mockpfx-ids-spanid": "50", "mockpfx-ids-traceid": "47"},
		},
	}
	for _, test := range testCases {
		t.Run(test.encoding, func(t *testing.T) {
			codec, err := ParseEncoding(test.encoding)
			if err != nil {
				t.Fatalf("ParseEncoding(%q) error = %v", test.encoding, err)
			}

			ctx := context.Background()
			parent := opentracing.StartSpan("job.parent")
			ctx = opentracing.ContextWithSpan(ctx, parent)

			span, _ := opentracing.StartSpanFromContext(ctx, "job.run")
			env := map[string]string{}
			if err := EncodeTraceContext(span, env, codec); err != nil {
				t.Fatalf("EncodeTraceContext(span, %v, %v) error = %v", env, codec, err)
			}
			if got := env[EnvVarTraceContextKey]; got == "" {
				t.Errorf("after EncodeTraceContext(span, env, %v): env[%q] = %q, want non-empty encoded trace context", codec, EnvVarTraceContextKey, got)
			}

			contextBytes, err := base64.URLEncoding.DecodeString(env[EnvVarTraceContextKey])
			if err != nil {
				t.Fatalf("base64.URLEncoding.DecodeString(%q) error = %v", env[EnvVarTraceContextKey], err)
			}

			dec := codec.NewDecoder(bytes.NewReader(contextBytes))
			textmap := opentracing.TextMapCarrier{}
			if err := dec.Decode(&textmap); err != nil {
				t.Fatalf("Codec(%v).NewDecoder(%q).Decode(&opentracing.TextMapCarrier{}) error = %v", codec, contextBytes, err)
			}
			// The content of the trace context will vary, but the keys should
			// remain the same.
			gotKeys := slices.Collect(maps.Keys(textmap))
			slices.Sort(gotKeys)

			wantKeys := slices.Collect(maps.Keys(test.want))
			slices.Sort(wantKeys)
			if diff := cmp.Diff(gotKeys, wantKeys); diff != "" {
				t.Errorf("decoded textmap keys diff (-got +want):\n%s", diff)
			}
		})
	}
}
