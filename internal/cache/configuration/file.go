package configuration

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// LoadFile reads a cache configuration YAML file and returns the cache
// definitions declared under its "dependencies" key. Returns an empty slice
// (and no error) when the file declares no dependencies.
func LoadFile(path string) ([]Cache, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read cache config file: %w", err)
	}

	var file struct {
		Dependencies []Cache `yaml:"dependencies"`
	}
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("failed to unmarshal cache config file: %w", err)
	}

	return file.Dependencies, nil
}
