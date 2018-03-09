package agent

import (
	"errors"
	"fmt"
	"os"
	"reflect"

	"github.com/buildkite/agent/env"
	"github.com/buildkite/interpolate"

	// This is a fork of gopkg.in/yaml.v2 that fixes anchors with MapSlice
	yaml "github.com/vinzenz/yaml"
)

type Pipeline struct {
	yaml.MapSlice
}

func (p Pipeline) MarshalJSON() ([]byte, error) {
	return nil, errors.New("Nope")
}

type PipelineParser struct {
	Env      *env.Environment
	Filename string
	Pipeline []byte
}

func (p PipelineParser) Parse() (interface{}, error) {
	if p.Env == nil {
		p.Env = env.FromSlice(os.Environ())
	}

	var pipelineAsMap map[string]interface{}

	// Check we can parse this as a map, otherwise later inferences about map structures break
	if err := yaml.Unmarshal([]byte(p.Pipeline), &pipelineAsMap); err != nil {
		return nil, fmt.Errorf("Failed to parse %v", err)
	}

	var pipeline yaml.MapSlice

	// Initially we unmarshal this into a yaml.MapSlice so that we preserve the order of maps
	if err := yaml.Unmarshal([]byte(p.Pipeline), &pipeline); err != nil {
		return nil, fmt.Errorf("Failed to parse %v", err)
	}

	// Preprocess any env that are defined in the top level block and place them into env for
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

	// Now we roundtrip this back into YAML bytes and back into a generic interface{}
	// that works with all upstream code (which likes working with JSON). Specifically we
	// need to convert the map[interface{}]interface{}'s that YAML likes into JSON compatible
	// map[string]interface{}
	b, err := yaml.Marshal(interpolated)
	if err != nil {
		return nil, err
	}

	var result interface{}
	if err := unmarshalAsStringMap(b, &result); err != nil {
		return nil, err
	}

	return result, nil
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

// Unmarshal YAML to map[string]interface{} instead of map[interface{}]interface{}, such that
// we can Marshal cleanly into JSON
// Via https://github.com/go-yaml/yaml/issues/139#issuecomment-220072190
func unmarshalAsStringMap(in []byte, out interface{}) error {
	var res interface{}

	if err := yaml.Unmarshal(in, &res); err != nil {
		return err
	}
	*out.(*interface{}) = cleanupMapValue(res)

	return nil
}

func cleanupInterfaceArray(in []interface{}) []interface{} {
	res := make([]interface{}, len(in))
	for i, v := range in {
		res[i] = cleanupMapValue(v)
	}
	return res
}

func cleanupInterfaceMap(in map[interface{}]interface{}) map[string]interface{} {
	res := make(map[string]interface{})
	for k, v := range in {
		res[fmt.Sprintf("%v", k)] = cleanupMapValue(v)
	}
	return res
}

func cleanupMapValue(v interface{}) interface{} {
	switch v := v.(type) {
	case []interface{}:
		return cleanupInterfaceArray(v)
	case map[interface{}]interface{}:
		return cleanupInterfaceMap(v)
	case string:
		return v
	default:
		return fmt.Sprintf("%v", v)
	}
}
