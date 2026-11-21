// Package reuse implements the agent side of step reuse: hashing a step's
// declared input files, digesting and archiving its output paths.
package reuse

import (
	"fmt"

	"drjosh.dev/zzglob"
)

// ExitCodeInputUnavailable is the exit status of `buildkite-agent reuse
// restore`, and of the job, when an upstream step's output can't be restored.
const ExitCodeInputUnavailable = 94

// StringList converts a pipeline value that is either a single string or a
// list of strings into a slice of strings. A nil value yields a nil slice.
func StringList(value any) ([]string, error) {
	switch x := value.(type) {
	case nil:
		return nil, nil

	case string:
		return []string{x}, nil

	case []any:
		list := make([]string, 0, len(x))
		for i, item := range x {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("at index %d, the item had unsupported type %T", i, item)
			}
			list = append(list, s)
		}
		return list, nil

	case []string:
		return x, nil

	default:
		return nil, fmt.Errorf("the value had unsupported type %T", x)
	}
}

// CompilePatterns parses glob patterns using the same rules as `if_changed`.
func CompilePatterns(patterns []string) ([]*zzglob.Pattern, error) {
	globs := make([]*zzglob.Pattern, 0, len(patterns))
	for _, patt := range patterns {
		g, err := zzglob.Parse(patt)
		if err != nil {
			return nil, fmt.Errorf("while parsing glob pattern %q: %w", patt, err)
		}
		globs = append(globs, g)
	}
	return globs, nil
}

// ParsePatterns converts a string or list of strings (as found within
// `if_changed` or `inputs.files`) into a slice of parsed globs.
func ParsePatterns(value any) ([]*zzglob.Pattern, error) {
	patterns, err := StringList(value)
	if err != nil {
		return nil, err
	}
	if patterns == nil {
		return nil, nil
	}
	return CompilePatterns(patterns)
}

// matches reports whether path matches any include pattern and no exclude
// pattern.
func matches(path string, include, exclude []*zzglob.Pattern) bool {
	for _, g := range exclude {
		if g.Match(path) {
			return false
		}
	}
	for _, g := range include {
		if g.Match(path) {
			return true
		}
	}
	return false
}
