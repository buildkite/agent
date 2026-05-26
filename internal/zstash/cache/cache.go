package cache

import (
	"fmt"
	"regexp"
	"strings"
)

type Cache struct {
	// Template of the cache entry.
	Template string
	// The registry to use which defaults to "~".
	Registry string
	// ID of the cache entry to save.
	ID string
	// Key of the cache entry to save, this can be a template string.
	Key string
	// Fallback keys to use, this is a comma delimited list of key template strings.
	FallbackKeys []string
	// Paths to remove.
	Paths []string
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

	// Key validation: non-empty
	if strings.TrimSpace(c.Key) == "" {
		errors = append(errors, "key cannot be empty")
	}

	// FallbackKeys validation: no spaces allowed
	for i, fallbackKey := range c.FallbackKeys {
		if strings.TrimSpace(fallbackKey) == "" {
			errors = append(errors, fmt.Sprintf("fallback key at index %d cannot be empty", i))
		} else if strings.Contains(fallbackKey, " ") {
			errors = append(errors, fmt.Sprintf("fallback key at index %d cannot contain spaces: '%s'", i, fallbackKey))
		}
	}

	// Paths validation: at least one valid path
	if len(c.Paths) == 0 {
		errors = append(errors, "at least one path must be specified")
	} else {
		for i, path := range c.Paths {
			if strings.TrimSpace(path) == "" {
				errors = append(errors, fmt.Sprintf("path at index %d cannot be empty", i))
			} else if !isValidPath(path) {
				errors = append(errors, fmt.Sprintf("path at index %d is not valid: '%s'", i, path))
			}
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
