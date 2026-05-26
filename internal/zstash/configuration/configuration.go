package configuration

import (
	"embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/buildkite/agent/v3/internal/zstash/cache"
	"github.com/buildkite/agent/v3/internal/zstash/internal/key"
)

//go:embed templates.json
var templatesFile embed.FS

/*
Takes a list of cache configurations and expands them into a list of cache of resolved Cache objects. This does the following:

* Expands cache.Template with the template values from template.json

* Expands cache.Key using templatable arguments (such as id, agent.os, agent.arch, env, checksum etc)

* Expands cache.FallbackKeys using templatable arguments (such as id, agent.os, agent.arch, env, checksum etc)

* Expands cache.Paths using templatable arguments (such as id, agent.os, agent.arch, env, checksum etc)

Uses the OS environment variables for template expansion.
*/
func ExpandCacheConfiguration(caches []cache.Cache) ([]cache.Cache, error) {
	return expandCacheConfiguration(caches, nil)
}

/*
ExpandCacheConfigurationWithEnv expands cache configurations using a provided environment map
instead of reading from the OS environment. This is useful for library usage where the environment
is controlled programmatically.

Parameters:
  - caches: List of cache configurations to expand
  - env: Map of environment variables to use for template expansion

Returns the expanded cache configurations or an error if expansion fails.
*/
func ExpandCacheConfigurationWithEnv(caches []cache.Cache, env map[string]string) ([]cache.Cache, error) {
	return expandCacheConfiguration(caches, env)
}

func expandCacheConfiguration(caches []cache.Cache, env map[string]string) ([]cache.Cache, error) {
	templatesMap, err := loadTemplates()
	if err != nil {
		return nil, fmt.Errorf("failed to load templates: %w", err)
	}

	for i, cache := range caches {
		// Replace cache.Template with the template values from template.json
		if cache.Template != "" {
			cache, err = augmentTemplateWithCache(templatesMap, cache)
			if err != nil {
				return nil, fmt.Errorf("failed to augment template with cache: %w", err)
			}
		}

		// Replace cache.Key with the templatable arguments
		cache.Key, err = key.TemplateWithEnv(cache.ID, cache.Key, env)
		if err != nil {
			return nil, fmt.Errorf("failed to expand key: %w", err)
		}

		// Replace cache.FallbackKeys with the templatable arguments (such as id, agent.os, agent.arch, env, checksum etc)
		cache.FallbackKeys, err = expandStringsWithEnv(cache.ID, cache.FallbackKeys, env)
		if err != nil {
			return nil, fmt.Errorf("failed to expand fallback keys: %w", err)
		}

		// Replace cache.Paths with the templatable arguments (such as id, agent.os, agent.arch, env, checksum etc)
		cache.Paths, err = expandStringsWithEnv(cache.ID, cache.Paths, env)
		if err != nil {
			return nil, fmt.Errorf("failed to expand paths: %w", err)
		}

		// Validates the cache object
		if err := cache.Validate(); err != nil {
			return nil, fmt.Errorf("cache validation failed for ID %s: %w", cache.ID, err)
		}

		// Save the modified cache back to the slice
		caches[i] = cache
	}

	return caches, nil
}

/*
Loads the templates from templates.json as a map of template name to Cache object.
Map<string, Cache>
*/
func loadTemplates() (map[string]cache.Cache, error) {
	file, err := templatesFile.Open("templates.json")
	if err != nil {
		return nil, fmt.Errorf("failed to open template file: %w", err)
	}
	defer file.Close()

	decoder := json.NewDecoder(file)

	rawTemplates := make(map[string]interface{})
	err = decoder.Decode(&rawTemplates)
	if err != nil {
		return nil, fmt.Errorf("failed to parse template file: %w", err)
	}

	// Create a map of template name to Cache object
	templatesCacheMap := make(map[string]cache.Cache)

	for templateName, template := range rawTemplates {
		cache, err := extractTemplateCache(templateName, template)
		if err != nil {
			return nil, fmt.Errorf("failed to extract template %s: %w", templateName, err)
		}
		templatesCacheMap[templateName] = cache
	}

	return templatesCacheMap, nil
}

// extractTemplateCache safely extracts a cache.Cache from raw template data
func extractTemplateCache(templateName string, template interface{}) (cache.Cache, error) {
	templateMap, ok := template.(map[string]interface{})
	if !ok {
		return cache.Cache{}, fmt.Errorf("template %s is not a valid object", templateName)
	}

	key, err := extractStringField(templateMap, "key", true)
	if err != nil {
		return cache.Cache{}, fmt.Errorf("template %s: %w", templateName, err)
	}

	fallbackKeys, err := extractStringArrayField(templateMap, "fallback_keys", true)
	if err != nil {
		return cache.Cache{}, fmt.Errorf("template %s: %w", templateName, err)
	}

	paths, err := extractStringArrayField(templateMap, "paths", true)
	if err != nil {
		return cache.Cache{}, fmt.Errorf("template %s: %w", templateName, err)
	}

	return cache.Cache{
		ID:           "", // Templates do not have an ID
		Template:     "", // Templates do not have a Template
		Registry:     "", // Templates do not have a Registry
		Key:          key,
		FallbackKeys: fallbackKeys,
		Paths:        paths,
	}, nil
}

// extractStringField safely extracts a string field from a map
func extractStringField(m map[string]interface{}, field string, required bool) (string, error) {
	value, exists := m[field]
	if !exists {
		if required {
			return "", fmt.Errorf("missing required field '%s'", field)
		} else {
			return "", nil
		}
	}

	str, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("field '%s' must be a string, got %T", field, value)
	}

	return str, nil
}

// extractStringArrayField safely extracts a []string field from a map
func extractStringArrayField(m map[string]interface{}, field string, required bool) ([]string, error) {
	value, exists := m[field]
	if !exists {
		if required {
			return nil, fmt.Errorf("missing required field '%s'", field)
		} else {
			return nil, nil
		}
	}

	array, ok := value.([]interface{})
	if !ok {
		return nil, fmt.Errorf("field '%s' must be an array, got %T", field, value)
	}

	strings := make([]string, len(array))
	for i, item := range array {
		str, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("field '%s'[%d] must be a string, got %T", field, i, item)
		}
		strings[i] = str
	}

	return strings, nil
}

/*
Replace cache.Template with the template values from template.json
*/
func augmentTemplateWithCache(templatesMap map[string]cache.Cache, cache cache.Cache) (cache.Cache, error) {
	template, ok := templatesMap[cache.Template]
	if !ok {
		return cache, fmt.Errorf("template '%s' not found", cache.Template)
	}

	// Remove the template from the cache as it is no longer needed.
	template.Template = ""

	// Merge the cache options into the template.
	if cache.Registry != "" {
		template.Registry = cache.Registry
	}
	if cache.ID != "" {
		template.ID = cache.ID
	}
	if cache.Key != "" {
		template.Key = cache.Key
	}
	if len(cache.FallbackKeys) > 0 {
		template.FallbackKeys = cache.FallbackKeys
	}
	if len(cache.Paths) > 0 {
		template.Paths = cache.Paths
	}

	return template, nil
}

/*
Expands an array of strings with templatable arguments (such as id, agent.os, agent.arch, env, checksum etc)
Uses the provided environment map if not nil, otherwise uses OS environment.
*/
func expandStringsWithEnv(id string, stringsArray []string, env map[string]string) ([]string, error) {
	expandedStrings := make([]string, len(stringsArray))

	for n, stringTemplate := range stringsArray {

		// trim quotes and whitespace
		stringTemplate = strings.Trim(stringTemplate, "\"' \t")

		expandedString, err := key.TemplateWithEnv(id, stringTemplate, env)
		if err != nil {
			return nil, fmt.Errorf("failed to template key: %w", err)
		}

		expandedStrings[n] = expandedString
	}

	return expandedStrings, nil
}
