package clicommand

import (
	"fmt"
	"os"

	"github.com/buildkite/zstash/cache"
	"github.com/stretchr/testify/assert/yaml"
)

// use yaml to load cache configuration from file
func loadCacheConfiguration(cacheConfigFile string) ([]cache.Cache, error) {
	data, err := os.ReadFile(cacheConfigFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read cache config file: %w", err)
	}

	var caches []cache.Cache
	if err := yaml.Unmarshal(data, &caches); err != nil {
		return nil, fmt.Errorf("failed to unmarshal cache config file: %w", err)
	}

	return caches, nil
}
