package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/buildkite/agent/env"
	"github.com/buildkite/agent/logger"
	"github.com/buildkite/interpolate"
	"github.com/ghodss/yaml"
)

type PipelineParser struct {
	Env      *env.Environment
	Filename string
	Pipeline []byte
}

func (p PipelineParser) Parse() (pipeline interface{}, err error) {
	if p.Env == nil {
		p.Env = env.FromSlice(os.Environ())
	}

	// First try and figure out the format from the filename
	format, err := inferFormat(p.Pipeline, p.Filename)
	if err != nil {
		return nil, err
	}

	// Unmarshal the pipeline into an actual data structure
	unmarshaled, err := unmarshal(p.Pipeline, format)
	if err != nil {
		return nil, err
	}

	// Preprocess any env that are defined in the top level block and place them into env for
	// later interpolation. We do this a few times so that you can reference env vars in other env vars
	if unmarshaledMap, ok := unmarshaled.(map[string]interface{}); ok {
		if envMap, ok := unmarshaledMap["env"].(map[string]interface{}); ok {
			if err = p.interpolateEnvBlock(envMap); err != nil {
				return nil, err
			}
		}
	}

	// Recursivly go through the entire pipeline and perform environment
	// variable interpolation on strings
	interpolated, err := p.interpolate(unmarshaled)
	if err != nil {
		return nil, err
	}

	return interpolated, nil
}

func (p PipelineParser) interpolateEnvBlock(envMap map[string]interface{}) error {
	// do a first pass without interpolation
	for k, v := range envMap {
		switch tv := v.(type) {
		case string, int, bool:
			p.Env.Set(k, fmt.Sprintf("%v", tv))
		}
	}

	// next do a pass of interpolation and read the results
	for k, v := range envMap {
		switch tv := v.(type) {
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

func inferFormat(pipeline []byte, filename string) (string, error) {
	// If we have a filename, try and figure out a format from that
	if filename != "" {
		extension := filepath.Ext(filename)
		if extension == ".yaml" || extension == ".yml" {
			return "yaml", nil
		} else if extension == ".json" {
			return "json", nil
		}
	}

	// Boo...we couldn't figure it out based on the filename. Next we'll
	// use a very dirty and ugly way of detecting if the pipeline is JSON.
	// It's not nice...but seems to work really well for our use case!
	firstCharacter := string(strings.TrimSpace(string(pipeline))[0])
	if firstCharacter == "{" || firstCharacter == "[" {
		return "json", nil
	}

	// If nothing else could be figured out, then default to YAML
	return "yaml", nil
}

func unmarshal(pipeline []byte, format string) (interface{}, error) {
	var unmarshaled interface{}

	if format == "yaml" {
		logger.Debug("Parsing pipeline configuration as YAML")

		err := yaml.Unmarshal(pipeline, &unmarshaled)
		if err != nil {
			// Error messages from the YAML parser have this ugly
			// prefix, so I'll just strip it for the sake of the
			// "aesthetics"
			message := strings.Replace(fmt.Sprintf("%s", err), "error converting YAML to JSON: yaml: ", "", 1)

			return nil, fmt.Errorf("Failed to parse YAML: %s", message)
		}
	} else if format == "json" {
		logger.Debug("Parsing pipeline configuration as JSON")

		err := json.Unmarshal(pipeline, &unmarshaled)
		if err != nil {
			return nil, fmt.Errorf("Failed to parse JSON: %s", err)
		}
	} else {
		if format == "" {
			return nil, fmt.Errorf("No format was supplied")
		} else {
			return nil, fmt.Errorf("Unknown format `%s`", format)
		}
	}

	return unmarshaled, nil
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

			copy.SetMapIndex(key, copyValue)
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
