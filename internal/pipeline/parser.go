package pipeline

import (
	"errors"
	"io"
	"strings"

	"github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/internal/ordered"
	"github.com/buildkite/agent/v3/tracetools"
	"github.com/buildkite/interpolate"

	"gopkg.in/yaml.v3"
)

// Parse parses a pipeline and applies interpolation. If nil envMap is provided,
// interpolation is skipped. Otherwise, Parse mutates envMap to contain
// variables from the pipeline's env block.
// To interpolate only using variables defined the pipeline, pass an empty but
// non-nil envMap.
func Parse(src io.Reader, envMap *env.Environment) (*Pipeline, error) {
	// Run the pipeline through the YAML parser.
	pipeline := new(Pipeline)
	if err := yaml.NewDecoder(src).Decode(pipeline); err != nil {
		return nil, formatYAMLError(err)
	}

	// No interpolation? No more to do.
	if envMap == nil {
		return pipeline, nil
	}

	// Propagate distributed tracing context to the new pipelines if available
	if tracing, has := envMap.Get(tracetools.EnvVarTraceContextKey); has {
		if pipeline.Env == nil {
			pipeline.Env = ordered.NewMap[string, string](1)
		}
		pipeline.Env.Set(tracetools.EnvVarTraceContextKey, tracing)
	}

	// Preprocess any env that are defined in the top level block and place them
	// into env for later interpolation into the rest of the pipeline.
	if err := interpolateEnvBlock(envMap, pipeline.Env); err != nil {
		return nil, err
	}

	// Recursively go through the entire pipeline and perform environment
	// variable interpolation on strings. Interpolation is performed in-place.
	if err := pipeline.interpolate(envMap); err != nil {
		return nil, err
	}

	return pipeline, nil
}

// interpolateEnvBlock runs Interpolate on each string value in envBlock,
// interpolating with the variables defined in envMap, and then adding the
// results back into both envBlock and envMap. Each environment variable can
// be interpolated into later environment variables, making the input ordering
// potentially important.
func interpolateEnvBlock(envMap *env.Environment, envBlock *ordered.Map[string, string]) error {
	return envBlock.Range(func(k, v string) error {
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

		envBlock.Replace(k, intk, intv)

		// Bonus part for the env block!
		// Add the results back into envMap.
		envMap.Set(intk, intv)
		return nil
	})
}

func formatYAMLError(err error) error {
	return errors.New(strings.TrimPrefix(err.Error(), "yaml: "))
}
