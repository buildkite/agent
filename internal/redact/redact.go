// Package redact provides functions for determining values to redact.
package redact

import (
	"fmt"
	"path"
	"slices"

	"github.com/buildkite/agent/v3/env"
)

// LengthMin is the shortest string length that will be considered a
// potential secret by the environment redactor. e.g. if the redactor is
// configured to filter out environment variables matching *_TOKEN, and
// API_TOKEN is set to "none", this minimum length will prevent the word "none"
// from being redacted from useful log output.
const LengthMin = 6

var redacted = []byte("[REDACTED]")

// Redact ignores its input and returns "[REDACTED]".
func Redact([]byte) []byte {
	return redacted
}

// MatchAny reports if the name matches any of the patterns.
func MatchAny(patterns []string, name string) (matched bool, err error) {
	// Track patterns that couldn't be parsed by path.Match, and report them
	// in a single error.
	var badPatterns []string
	defer func() {
		if len(badPatterns) > 0 {
			slices.Sort(badPatterns)
			err = fmt.Errorf("bad patterns: %q", badPatterns)
		}
	}()

	for _, pattern := range patterns {
		matched, err := path.Match(pattern, name)
		if err != nil {
			badPatterns = append(badPatterns, pattern)
			continue
		}

		if matched {
			return true, nil
		}
	}
	return false, nil
}

// Vars returns the variable names and values to be redacted, given a
// redaction config string and an environment map. It also returned variables
// whose names match the redacted-vars config, but whose values were too short.
func Vars(patterns []string, environment []env.Pair) (matched []env.Pair, short []string, err error) {
	for _, pair := range environment {
		// Does the name match any of the patterns?
		m, err := MatchAny(patterns, pair.Name)
		if err != nil {
			return nil, nil, err
		}
		if !m {
			continue
		}

		// The name matched, now test the length of the value.
		if len(pair.Value) < LengthMin {
			if len(pair.Value) > 0 {
				short = append(short, pair.Name)
			}
			continue
		}

		matched = append(matched, pair)
	}

	return matched, short, nil
}
