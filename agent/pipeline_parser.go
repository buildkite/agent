package agent

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"

	"github.com/buildkite/agent/envvar"
	"github.com/buildkite/agent/logger"
	"github.com/ghodss/yaml"
)

type PipelineParser struct {
	Filename string
	Format   string
	Pipeline []byte
}

func (p PipelineParser) Parse() (pipeline interface{}, err error) {
	format := p.Format

	// If no format is passed, figure it out based on the filename
	if format == "" {
		logger.Debug("Pipeline format not supplied, inferring from filename `%s`", p.Filename)

		format, err = inferFormatFromFilename(p.Filename)
		if err != nil {
			return nil, err
		}
	}

	// Unmarshal the pipeline into an actual data structure
	unmarshaled, err := unmarshal(p.Pipeline, format)
	if err != nil {
		return nil, err
	}

	// Recursivly go through the entire pipeline and perform environment
	// variable interpolation on strings
	interpolated, err := interpolate(unmarshaled)
	if err != nil {
		return nil, err
	}

	return interpolated, nil
}

func inferFormatFromFilename(filename string) (string, error) {
	// Make sure we've got a filename in the first place
	if filename == "" {
		return "", fmt.Errorf("No filename to infer a format from")
	}

	// Get the file extension
	extension := filepath.Ext(filename)
	if extension == "" {
		return "", fmt.Errorf("No extension could be inferred from filename `%s`", filename)
	}

	// Figure out the format from the extension
	if extension == ".yaml" || extension == ".yml" {
		return "yaml", nil
	} else if extension == ".json" {
		return "json", nil
	} else {
		return "", fmt.Errorf("Could not infer a pipeline from `%s` with extension `%s`. To force a format, please use `--format`", filename, extension)
	}
}

func unmarshal(pipeline []byte, format string) (interface{}, error) {
	var unmarshaled interface{}

	if format == "yaml" {
		logger.Debug("Parsing pipeline configuration as YAML")

		err := yaml.Unmarshal(pipeline, &unmarshaled)
		if err != nil {
			return nil, fmt.Errorf("Failed to parse YAML: %s", err)
		}
	} else if format == "json" {
		logger.Debug("Parsing pipeline configuration as JSON")

		err := json.Unmarshal(pipeline, &unmarshaled)
		if err != nil {
			return nil, fmt.Errorf("Failed to parse JSON: %s", err)
		}
	} else {
		if format == "" {
			return nil, fmt.Errorf("No format was supplied or one could not be inferred")
		} else {
			return nil, fmt.Errorf("Unknown format `%s`", format)
		}
	}

	return unmarshaled, nil
}

// interpolate function inspired from: https://gist.github.com/hvoecking/10772475

func interpolate(obj interface{}) (interface{}, error) {
	// Wrap the original in a reflect.Value
	original := reflect.ValueOf(obj)

	copy := reflect.New(original.Type()).Elem()

	err := interpolateRecursive(copy, original)
	if err != nil {
		return nil, err
	}

	// Remove the reflection wrapper
	return copy.Interface(), nil
}

func interpolateRecursive(copy, original reflect.Value) error {
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
		err := interpolateRecursive(copy.Elem(), originalValue)
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

		// Create a new object. Now new gives us a pointer, but we want the value it
		// points to, so we have to call Elem() to unwrap it
		copyValue := reflect.New(originalValue.Type()).Elem()

		err := interpolateRecursive(copyValue, originalValue)
		if err != nil {
			return err
		}

		copy.Set(copyValue)

	// If it is a struct we interpolate each field
	case reflect.Struct:
		for i := 0; i < original.NumField(); i += 1 {
			err := interpolateRecursive(copy.Field(i), original.Field(i))
			if err != nil {
				return err
			}
		}

	// If it is a slice we create a new slice and interpolate each element
	case reflect.Slice:
		copy.Set(reflect.MakeSlice(original.Type(), original.Len(), original.Cap()))

		for i := 0; i < original.Len(); i += 1 {
			err := interpolateRecursive(copy.Index(i), original.Index(i))
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
			err := interpolateRecursive(copyValue, originalValue)
			if err != nil {
				return err
			}

			copy.SetMapIndex(key, copyValue)
		}

	// If it is a string interpolate it (yay finally we're doing what we came for)
	case reflect.String:
		interpolated, err := envvar.Interpolate(original.Interface().(string))
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
