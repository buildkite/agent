package pipeline

import (
	"errors"
	"fmt"
	"strings"

	"github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/internal/ordered"
	"github.com/buildkite/agent/v3/tracetools"
	"github.com/buildkite/interpolate"

	"gopkg.in/yaml.v3"
)

// Parser parses a pipeline, optionally interpolating values from
// a given environment.
type Parser struct {
	Env             *env.Environment
	Filename        string
	PipelineSource  []byte
	NoInterpolation bool
}

// Parse runs the parser.
func (p *Parser) Parse() (*Pipeline, error) {
	if p.Env == nil {
		p.Env = env.New()
	}

	errPrefix := "Failed to parse pipeline"
	if p.Filename != "" {
		errPrefix = fmt.Sprintf("Failed to parse %s", p.Filename)
	}

	// Run the pipeline through the YAML parser.
	pipeline := new(Pipeline)
	if err := yaml.Unmarshal(p.PipelineSource, pipeline); err != nil {
		return nil, fmt.Errorf("%s: %v", errPrefix, formatYAMLError(err))
	}

	// No interpolation? No more to do.
	if p.NoInterpolation {
		return pipeline, nil
	}

	// Propagate distributed tracing context to the new pipelines if available
	if tracing, has := p.Env.Get(tracetools.EnvVarTraceContextKey); has {
		if pipeline.Env == nil {
			pipeline.Env = ordered.NewMap[string, string](1)
		}
		pipeline.Env.Set(tracetools.EnvVarTraceContextKey, tracing)
	}

	// Preprocess any env that are defined in the top level block and place them
	// into env for later interpolation into env blocks
	if err := p.interpolateEnvBlock(pipeline.Env); err != nil {
		return nil, err
	}

	// Recursively go through the entire pipeline and perform environment
	// variable interpolation on strings. Interpolation is performed in-place.
	if err := pipeline.interpolate(p); err != nil {
		return nil, err
	}

	return pipeline, nil
}

// interpolateEnvBlock runs Interpolate on each string value in the envMap,
// interpolating with the variables defined in p.Env, and then adding the
// results back into to p.Env.
func (p *Parser) interpolateEnvBlock(env *ordered.Map[string, string]) error {
	return env.Range(func(k, v string) error {
		// We interpolate both keys and values.
		intk, err := interpolate.Interpolate(p.Env, k)
		if err != nil {
			return err
		}

		// v is always a string in this case.
		intv, err := p.interpolateStr(v)
		if err != nil {
			return err
		}

		env.Replace(k, intk, intv)

		// Bonus part for the env block!
		// Add the results back into p.Env.
		p.Env.Set(intk, intv)
		return nil
	})
}

func formatYAMLError(err error) error {
	return errors.New(strings.TrimPrefix(err.Error(), "yaml: "))
}

// selfInterpolater describes types that can interpolate themselves in-place.
// They can call the parser's interpolateStr and interpolateAny on their
// contents to do this.
type selfInterpolater interface {
	interpolate(*Parser) error
}

// interpolateStr is a convenience function that returns
// interpolate.Interpolate(p.Env, s).
func (p *Parser) interpolateStr(s string) (string, error) {
	return interpolate.Interpolate(p.Env, s)
}

// interpolateAny interpolates (almost) anything in-place. It returns the same
// type it is passed. When passed a string, it returns a new string. Anything
// it doesn't know how to interpolate is returned unaltered.
func (p *Parser) interpolateAny(o any) (any, error) {
	// Either it is a basic type produced by the yaml package, or it is one of
	// our types (which should implement selfInterpolater).
	switch o := o.(type) {
	case string:
		return interpolate.Interpolate(p.Env, o)

	case []any:
		return o, interpolateSlice(p, o)

	case []string:
		return o, interpolateSlice(p, o)

	case map[string]any:
		return o, interpolateMap(p, o)

	case map[string]string:
		return o, interpolateMap(p, o)

	case *ordered.Map[string, any]:
		return o, interpolateOrderedMap(p, o)

	case *ordered.Map[string, string]:
		return o, interpolateOrderedMap(p, o)

	case selfInterpolater:
		return o, o.interpolate(p)

	default:
		return o, nil
	}
}

func interpolateSlice[E any, S ~[]E](p *Parser, s S) error {
	for i, e := range s {
		// It could be a string, so replace the old value with the new.
		inte, err := p.interpolateAny(e)
		if err != nil {
			return err
		}
		if inte == nil {
			// Then e was nil to begin with. No need to update it.
			continue
		}
		s[i] = inte.(E)
	}
	return nil
}

func interpolateMap[V any, M ~map[string]V](p *Parser, m M) error {
	for k, v := range m {
		// We interpolate both keys and values.
		intk, err := p.interpolateStr(k)
		if err != nil {
			return err
		}

		// V could be string, so be sure to replace the old value with the new.
		intv, err := p.interpolateAny(v)
		if err != nil {
			return err
		}

		// If the key changed due to interpolation, delete the old key.
		if k != intk {
			delete(m, k)
		}
		if intv == nil {
			// Then v was nil to begin with.
			// In case we're changing keys, we should reassign.
			// But we don't know the type, so can't assign nil.
			// Fortunately, v itself must be the right type.
			m[intk] = v
			continue
		}
		m[intk] = intv.(V)
	}
	return nil
}

func interpolateOrderedMap[K comparable, V any](p *Parser, m *ordered.Map[K, V]) error {
	return m.Range(func(k K, v V) error {
		// We interpolate both keys and values.
		intk, err := p.interpolateAny(k)
		if err != nil {
			return err
		}
		intv, err := p.interpolateAny(v)
		if err != nil {
			return err
		}

		// interpolateAny preserves the type, so these assertions are safe.
		m.Replace(k, intk.(K), intv.(V))
		return nil
	})
}
