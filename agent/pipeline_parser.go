package agent

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/tracetools"
	"github.com/buildkite/agent/v3/yamltojson"
	"github.com/buildkite/interpolate"

	// This is a fork of gopkg.in/yaml.v2 that fixes anchors with MapSlice
	yaml "github.com/buildkite/yaml"
)

type PipelineParser struct {
	Env             *env.Environment
	Filename        string
	Pipeline        []byte
	NoInterpolation bool
}

func (p PipelineParser) Parse() (*PipelineParserResult, error) {
	if p.Env == nil {
		p.Env = env.New()
	}

	var errPrefix string
	if p.Filename == "" {
		errPrefix = "Failed to parse pipeline"
	} else {
		errPrefix = fmt.Sprintf("Failed to parse %s", p.Filename)
	}

	var pipelineAsSlice []topLevelStep
	var pipeline yaml.MapSlice

	// We support top-level arrays of steps, so try that first
	if err := yaml.Unmarshal(p.Pipeline, &pipelineAsSlice); err == nil {
		var steps []interface{}

		// Unwrap our custom topLevelStep types for marshaling later
		for _, step := range pipelineAsSlice {
			if step.MapSlice != nil {
				steps = append(steps, step.MapSlice)
			} else {
				steps = append(steps, step.Body)
			}
		}

		pipeline = yaml.MapSlice{
			{Key: "steps", Value: steps},
		}
	} else if err := yaml.Unmarshal(p.Pipeline, &pipeline); err != nil {
		return nil, fmt.Errorf("%s: %v", errPrefix, formatYAMLError(err))
	}

	if p.NoInterpolation {
		return &PipelineParserResult{pipeline: pipeline}, nil
	}

	// Propagate distributed tracing context to the new pipelines if available
	if tracing, has := p.Env.Get(tracetools.EnvVarTraceContextKey); has {
		var envVars yaml.MapSlice
		if envMap, has := mapSliceItem("env", pipeline); has {
			envVars = envMap.Value.(yaml.MapSlice)
		} else {
			envVars = yaml.MapSlice{}
		}
		envVars = append(envVars, yaml.MapItem{
			Key:   tracetools.EnvVarTraceContextKey,
			Value: tracing,
		})
		// Since the actual env vars MapSlice is nested under the top level MapSlice,
		// updating the env vars doesn't actually update the pipeline. So we have to
		// replace it.
		pipeline = upsertSliceItem("env", pipeline, envVars)
	}

	// Preprocess any env that are defined in the top level block and place them into env for
	// later interpolation into env blocks
	if item, ok := mapSliceItem("env", pipeline); ok {
		if envMap, ok := item.Value.(yaml.MapSlice); ok {
			if err := p.interpolateEnvBlock(envMap); err != nil {
				return nil, err
			}
		} else {
			return nil, fmt.Errorf("Expected pipeline top-level env block to be a map, got %T", item)
		}
	}

	// Recursively go through the entire pipeline and perform environment
	// variable interpolation on strings
	interpolated, err := p.interpolate(pipeline)
	if err != nil {
		return nil, err
	}

	return &PipelineParserResult{pipeline: interpolated.(yaml.MapSlice)}, nil
}

// upsertSliceItem will replace a key's value in the given MapSlice with the given
// replacement or insert it if it doesn't exist.
func upsertSliceItem(key string, s yaml.MapSlice, val interface{}) yaml.MapSlice {
	found := -1
	for i, item := range s {
		if k, ok := item.Key.(string); ok && k == key {
			found = i
			break
		}
	}
	if found != -1 {
		s[found].Value = val
	} else {
		s = append(s, yaml.MapItem{
			Key:   key,
			Value: val,
		})
	}
	return s
}

func mapSliceItem(key string, s yaml.MapSlice) (yaml.MapItem, bool) {
	for _, item := range s {
		if k, ok := item.Key.(string); ok && k == key {
			return item, true
		}
	}
	return yaml.MapItem{}, false
}

func (p PipelineParser) interpolateEnvBlock(envMap yaml.MapSlice) error {
	for _, item := range envMap {
		k, ok := item.Key.(string)
		if !ok {
			return fmt.Errorf("Unexpected type of %T for env block key %v", item.Key, item.Key)
		}
		switch tv := item.Value.(type) {
		case string:
			interpolated, err := interpolate.Interpolate(p.Env, tv)
			if err != nil {
				return err
			}
			p.Env.Set(k, interpolated)
		}
	}
	return nil
}

func formatYAMLError(err error) error {
	return errors.New(strings.TrimPrefix(err.Error(), "yaml: "))
}

// interpolate function inspired from: https://gist.github.com/hvoecking/10772475

func (p PipelineParser) interpolate(obj interface{}) (interface{}, error) {
	// Make sure there's something actually to interpolate
	if obj == nil {
		return nil, nil
	}

	// Wrap the original in a reflect.Value
	original := reflect.ValueOf(obj)

	// Make a copy that we'll add the new values to
	copy := reflect.New(original.Type()).Elem()

	err := p.interpolateRecursive(copy, original)
	if err != nil {
		return nil, err
	}

	// Remove the reflection wrapper
	return copy.Interface(), nil
}

func (p PipelineParser) interpolateRecursive(copy, original reflect.Value) error {
	switch original.Kind() {
	// If it is a pointer we need to unwrap and call once again
	case reflect.Ptr:
		// To get the actual value of the original we have to call Elem()
		// At the same time this unwraps the pointer so we don't end up in
		// an infinite recursion
		originalValue := original.Elem()

		// Check if the pointer is nil
		if !originalValue.IsValid() {
			return nil
		}

		// Allocate a new object and set the pointer to it
		copy.Set(reflect.New(originalValue.Type()))

		// Unwrap the newly created pointer
		err := p.interpolateRecursive(copy.Elem(), originalValue)
		if err != nil {
			return err
		}

	// If it is an interface (which is very similar to a pointer), do basically the
	// same as for the pointer. Though a pointer is not the same as an interface so
	// note that we have to call Elem() after creating a new object because otherwise
	// we would end up with an actual pointer
	case reflect.Interface:
		// Get rid of the wrapping interface
		originalValue := original.Elem()

		// Check to make sure the interface isn't nil
		if !originalValue.IsValid() {
			return nil
		}

		// Create a new object. Now new gives us a pointer, but we want the value it
		// points to, so we have to call Elem() to unwrap it
		copyValue := reflect.New(originalValue.Type()).Elem()

		err := p.interpolateRecursive(copyValue, originalValue)
		if err != nil {
			return err
		}

		copy.Set(copyValue)

	// If it is a struct we interpolate each field
	case reflect.Struct:
		for i := 0; i < original.NumField(); i += 1 {
			err := p.interpolateRecursive(copy.Field(i), original.Field(i))
			if err != nil {
				return err
			}
		}

	// If it is a slice we create a new slice and interpolate each element
	case reflect.Slice:
		copy.Set(reflect.MakeSlice(original.Type(), original.Len(), original.Cap()))

		for i := 0; i < original.Len(); i += 1 {
			err := p.interpolateRecursive(copy.Index(i), original.Index(i))
			if err != nil {
				return err
			}
		}

	// If it is a map we create a new map and interpolate each value
	case reflect.Map:
		copy.Set(reflect.MakeMap(original.Type()))

		for _, key := range original.MapKeys() {
			originalValue := original.MapIndex(key)

			// New gives us a pointer, but again we want the value
			copyValue := reflect.New(originalValue.Type()).Elem()
			err := p.interpolateRecursive(copyValue, originalValue)
			if err != nil {
				return err
			}

			// Also interpolate the key if it's a string
			if key.Kind() == reflect.String {
				interpolatedKey, err := interpolate.Interpolate(p.Env, key.Interface().(string))
				if err != nil {
					return err
				}
				copy.SetMapIndex(reflect.ValueOf(interpolatedKey), copyValue)
			} else {
				copy.SetMapIndex(key, copyValue)
			}
		}

	// If it is a string interpolate it (yay finally we're doing what we came for)
	case reflect.String:
		interpolated, err := interpolate.Interpolate(p.Env, original.Interface().(string))
		if err != nil {
			return err
		}
		copy.SetString(interpolated)

	// And everything else will simply be taken from the original
	default:
		copy.Set(original)
	}

	return nil
}

// PipelineParserResult is the ordered parse tree of a Pipeline document
type PipelineParserResult struct {
	pipeline yaml.MapSlice
}

func (p *PipelineParserResult) MarshalJSON() ([]byte, error) {
	return yamltojson.MarshalMapSliceJSON(p.pipeline)
}

// topLevelStep is a custom type to support "step or string" which works around
// an issue where ordered parsing of yaml doesn't work with a top-level slice
type topLevelStep struct {
	yaml.MapSlice
	Body string
}

func (s *topLevelStep) UnmarshalYAML(unmarshal func(interface{}) error) error {
	// Some steps are plain old strings like "wait". To avoid unmarshaling errors
	// we check for plain old strings
	if err := unmarshal(&s.Body); err == nil {
		return nil
	}
	return unmarshal(&s.MapSlice)
}
