package configuration

import (
	"fmt"
	"sort"
	"strings"
)

const (
	ScopeBranch   = "branch"
	ScopeBuildID  = "build"
	ScopePipeline = "pipeline"
)

var saveScopeEnvVars = map[string]string{
	ScopeBranch:   "BUILDKITE_BRANCH",
	ScopeBuildID:  "BUILDKITE_BUILD_ID",
	ScopePipeline: "BUILDKITE_PIPELINE_SLUG",
}

// ResolveSaveScopes resolves the cache's save_scopes by
// preserving enabled scopes and removing disabled scopes.
// The result is always non-nil so the wire scopes object
// marshals to {} rather than null.
func ResolveSaveScopes(scopes map[string]bool) map[string]bool {
	resolved := make(map[string]bool, len(scopes))
	for scope, enabled := range scopes {
		if !enabled {
			continue
		}
		resolved[scope] = enabled
	}
	return resolved
}

func (c Cache) validateSaveScopes(errors []string) []string {
	if len(c.SaveScopes) > 0 {
		var unknown []string
		for s := range c.SaveScopes {
			if _, ok := saveScopeEnvVars[s]; !ok {
				unknown = append(unknown, s)
			}
		}

		if len(unknown) > 0 {
			sort.Strings(unknown)
			errors = append(errors, fmt.Sprintf("save_scopes: unsupported scope(s) '%s' (supported: branch, build, pipeline)", strings.Join(unknown, ", ")))
		}
	}
	return errors
}
