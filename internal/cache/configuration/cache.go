package configuration

import (
	"fmt"
	"regexp"
	"strings"
)

type Cache struct {
	// ID of the cache entry to save.
	ID string `yaml:"id"`
	// Key of the cache entry to save, this can be a template string.
	CacheKey []KeyPart `yaml:"cache_key"`
	// Target Paths to remove.
	TargetPaths []string `yaml:"target_paths"`
}

// Validate validates the cache configuration and returns an error if invalid.
func (c Cache) Validate() error {
	var errors []string

	// ID validation: alphanumeric and underscore only
	if strings.TrimSpace(c.ID) == "" {
		errors = append(errors, "id cannot be empty")
	} else if !regexp.MustCompile(`^[a-zA-Z0-9_]+$`).MatchString(c.ID) {
		errors = append(errors, fmt.Sprintf("id '%s' can only contain letters, numbers, and underscores", c.ID))
	}

	// Cache Key validation: non-empty
	if len(c.CacheKey) == 0 {
		errors = append(errors, "cache_key cannot be empty")
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
		return fmt.Errorf("cache validation failed for id '%s': %s", c.ID, strings.Join(errors, "; "))
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
