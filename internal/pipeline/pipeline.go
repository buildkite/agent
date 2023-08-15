package pipeline

import (
	"fmt"

	"github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/internal/ordered"
	"github.com/buildkite/agent/v3/tracetools"
	"github.com/buildkite/interpolate"
	"github.com/lestrrat-go/jwx/v2/jwk"
)

// Pipeline models a pipeline.
//
// Standard caveats apply - see the package comment.
type Pipeline struct {
	Steps Steps          `yaml:"steps"`
	Env   *ordered.MapSS `yaml:"env,omitempty"`

	// RemainingFields stores any other top-level mapping items so they at least
	// survive an unmarshal-marshal round-trip.
	RemainingFields map[string]any `yaml:",inline"`
}

// MarshalJSON marshals a pipeline to JSON. Special handling is needed because
// yaml.v3 has "inline" but encoding/json has no concept of it.
func (p *Pipeline) MarshalJSON() ([]byte, error) {
	return inlineFriendlyMarshalJSON(p)
}

// UnmarshalOrdered unmarshals the pipeline from either []any (a legacy
// sequence of steps) or *ordered.MapSA (a modern pipeline configuration).
func (p *Pipeline) UnmarshalOrdered(o any) error {
	switch o := o.(type) {
	case *ordered.MapSA:
		// A pipeline can be a mapping.
		// Wrap in a secret type to avoid infinite recursion between this method
		// and ordered.Unmarshal.
		type wrappedPipeline Pipeline
		if err := ordered.Unmarshal(o, (*wrappedPipeline)(p)); err != nil {
			return fmt.Errorf("unmarshaling Pipeline: %w", err)
		}

	case []any:
		// A pipeline can be a sequence of steps.
		if err := ordered.Unmarshal(o, &p.Steps); err != nil {
			return fmt.Errorf("unmarshaling steps: %w", err)
		}

	default:
		return fmt.Errorf("unmarshaling Pipeline: unsupported type %T, want either *ordered.Map[string, any] or []any", o)
	}

	// Ensure Steps is never nil. Server side expects a sequence.
	if p.Steps == nil {
		p.Steps = Steps{}
	}
	return nil
}

// Interpolate interpolates variables defined in both envMap and p.Env into the
// pipeline.
// More specifically, it does these things:
//   - Copy tracing context keys from envMap into pipeline.Env.
//   - Interpolate pipeline.Env and copy the results into envMap to apply later.
//   - Interpolate any string value in the rest of the pipeline.
func (p *Pipeline) Interpolate(envMap *env.Environment) error {
	if envMap == nil {
		envMap = env.New()
	}

	// Propagate distributed tracing context to the new pipelines if available
	if tracing, has := envMap.Get(tracetools.EnvVarTraceContextKey); has {
		if p.Env == nil {
			p.Env = ordered.NewMap[string, string](1)
		}
		p.Env.Set(tracetools.EnvVarTraceContextKey, tracing)
	}

	// Preprocess any env that are defined in the top level block and place them
	// into env for later interpolation into the rest of the pipeline.
	if err := p.interpolateEnvBlock(envMap); err != nil {
		return err
	}

	// Recursively go through the rest of the pipeline and perform environment
	// variable interpolation on strings. Interpolation is performed in-place.
	if err := interpolateSlice(envMap, p.Steps); err != nil {
		return err
	}
	return interpolateMap(envMap, p.RemainingFields)
}

// interpolateEnvBlock runs interpolate.Interpolate on each pair in p.Env,
// interpolating with the variables defined in envMap, and then adding the
// results back into both p.Env and envMap. Each environment variable can
// be interpolated into later environment variables, making the input ordering
// of p.Env potentially important.
func (p *Pipeline) interpolateEnvBlock(envMap *env.Environment) error {
	return p.Env.Range(func(k, v string) error {
		// We interpolate both keys and values.
		intk, err := interpolate.Interpolate(envMap, k)
		if err != nil {
			return err
		}

		// v is always a string in this case.
		intv, err := interpolate.Interpolate(envMap, v)
		if err != nil {
			return err
		}

		p.Env.Replace(k, intk, intv)

		// Bonus part for the env block!
		// Add the results back into envMap.
		envMap.Set(intk, intv)
		return nil
	})
}

// Sign signs each signable part of the pipeline. Currently this is limited to
// command steps (including command steps within group steps), including all
// plugin configurations and the pipeline "env". Parts of the pipeline are
// mutated directly, so an error part-way through may leave some steps
// un-signed.
func (p *Pipeline) Sign(key jwk.Key) error {
	return p.Steps.sign(p.Env.ToMap(), key)
}
