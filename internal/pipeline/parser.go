package pipeline

import (
	"errors"
	"io"
	"strings"

	"gopkg.in/yaml.v3"
)

// Parse parses a pipeline. It does not apply interpolation.
func Parse(src io.Reader) (*Pipeline, error) {
	pipeline := new(Pipeline)
	if err := yaml.NewDecoder(src).Decode(pipeline); err != nil {
		return nil, formatYAMLError(err)
	}
	return pipeline, nil
}

func formatYAMLError(err error) error {
	return errors.New(strings.TrimPrefix(err.Error(), "yaml: "))
}
