package pipeline

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/buildkite/agent/v3/env"
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
			pipeline.Env = make(map[string]string)
		}
		pipeline.Env[tracetools.EnvVarTraceContextKey] = tracing
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
func (p *Parser) interpolateEnvBlock(env map[string]string) error {
	// We interpolate both keys and values.
	// For simplicity, only interpolate the original p.Env into keys.
	for k, v := range env {
		intk, err := interpolate.Interpolate(p.Env, k)
		if err != nil {
			return err
		}

		// If the key changed due to interpolation, update it.
		if k == intk {
			continue
		}
		delete(env, k)
		env[intk] = v
	}

	// Iteration order flakes the interpolation of env vars in other env vars.
	// Instead of relying on order, let's do a topological sort. That way we can
	// write variables out of order, and they'll still interpolate in one
	// another as long as there are no dependency cycles!

	next := make(map[string][]string)
	pred := make(map[string]int, len(env))
	for k, v := range env {
		// Ensure all keys are present in pred.
		pred[k] = 0

		ids, err := interpolate.Identifiers(v)
		if err != nil {
			return err
		}

		for _, id := range ids {
			if _, known := p.Env.Get(id); known {
				// if we already know what id is (it's in p.Env), skip it
				continue
			}
			// id is a predecessor of k - resolving id helps resolve k
			pred[k]++
			next[id] = append(next[id], k)
		}
	}

	for len(pred) > 0 {
		// Find all the keys with no predecessors, enqueue them
		queue := make([]string, 0, len(pred))
		for k, c := range pred {
			if c == 0 {
				queue = append(queue, k)
			}
		}

		// Queue empty? Uh oh! We found a cycle.
		if len(queue) == 0 {
			// O(n) find the least destructive way to break the cycle:
			// The key with the fewest successors.
			var bestK string
			minnext := math.MaxInt
			for k, ns := range next {
				if len(ns) < minnext {
					bestK = k
				}
			}
			queue = append(queue, bestK)
		}

		// Process the queue, enqueueing keys that are resolved as we go
		for len(queue) > 0 {
			k := queue[0]
			queue = queue[1:]
			delete(pred, k)

			// Resolve k
			intv, err := interpolate.Interpolate(p.Env, env[k])
			if err != nil {
				return err
			}
			p.Env.Set(k, intv)

			for _, id := range next[k] {
				// Decrement pred[id], but only if id is still in pred.
				// This is only needed in the cycle case.
				_, ok := pred[id]
				if !ok {
					continue
				}
				pred[id]--
				if pred[id] <= 0 {
					queue = append(queue, id)
				}
			}
		}
	}
	return nil
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
