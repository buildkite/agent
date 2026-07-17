package configuration

import (
	"fmt"
	"regexp"
	"strings"
)

type Cache struct {
	// Name of the cache entry to save.
	Name string `yaml:"name"`
	// Key of the cache entry to save, this can be a template string.
	CacheKey []KeyPart `yaml:"cache_key"`
	// Target Paths to remove.
	TargetPaths []string `yaml:"target_paths"`
}

// Validate validates the cache configuration and returns an error if invalid.
func (c Cache) Validate() error {
	var errors []string

	// Name validation: alphanumeric and underscore only
	if strings.TrimSpace(c.Name) == "" {
		errors = append(errors, "name cannot be empty")
	} else if !regexp.MustCompile(`^[a-zA-Z0-9_]+$`).MatchString(c.Name) {
		errors = append(errors, fmt.Sprintf("name '%s' can only contain letters, numbers, and underscores", c.Name))
	}

	// Cache Key validation: non-empty
	if len(c.CacheKey) == 0 {
		errors = append(errors, "cache_key cannot be empty")
	}

	// At most one cache_key part may declare fallback_limit.
	fallbackLimits := 0
	for _, part := range c.CacheKey {
		if part.FallbackLimit {
			fallbackLimits++
		}
	}
	if fallbackLimits > 1 {
		errors = append(errors, "cache_key: fallback_limit may be set on at most one part")
	}

	// TargetPaths validation: non-empty set of valid, unique paths
	if len(c.TargetPaths) == 0 {
		errors = append(errors, "at least one target_paths entry must be specified")
	} else {
		seen := make(map[string]struct{}, len(c.TargetPaths))
		for i, targetPath := range c.TargetPaths {
			switch {
			case strings.TrimSpace(targetPath) == "":
				errors = append(errors, fmt.Sprintf("target_paths[%d] cannot be empty", i))
			case !isValidPath(targetPath):
				errors = append(errors, fmt.Sprintf("target_paths[%d] is not valid: '%s'", i, targetPath))
			}
			if _, dup := seen[targetPath]; dup {
				errors = append(errors, fmt.Sprintf("target_paths[%d] '%s' is duplicated (target_paths is a set)", i, targetPath))
			}
			seen[targetPath] = struct{}{}
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("cache validation failed for name '%s': %s", c.Name, strings.Join(errors, "; "))
	}

	return nil
}

// isValidPath checks if a path is valid (doesn't contain null bytes or other invalid characters).
func isValidPath(path string) bool {
	// Check for invalid characters (null bytes, etc.)
	if strings.ContainsRune(path, 0) {
		return false
	}

	// Additional platform-specific validation could go here
	// For now, just check it's not empty and doesn't contain null bytes
	return strings.TrimSpace(path) != ""
}
