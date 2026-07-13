package configuration

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// LoadFile reads a cache configuration YAML file and returns the cache
// definitions declared under its "caches" key. Returns an empty slice
// (and no error) when the file declares no caches.
func LoadFile(path string) ([]Cache, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read cache config file: %w", err)
	}

	var file struct {
		Caches []Cache `yaml:"caches"`
	}
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("failed to unmarshal cache config file: %w", err)
	}

	return file.Caches, nil
}
