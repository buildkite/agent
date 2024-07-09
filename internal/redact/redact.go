// Package redact provides functions for determining values to redact.
package redact

import (
	"path"
	"sort"
	"strings"

	"github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/internal/job/shell"
	"golang.org/x/exp/maps"
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

// Match reports if the name matches any of the patterns.
func Match(logger shell.Logger, patterns []string, name string) bool {
	for _, pattern := range patterns {
		matched, err := path.Match(pattern, name)
		if err != nil {
			// path.ErrBadPattern is the only error returned by path.Match
			logger.Warningf("Bad redacted vars pattern: %s", pattern)
			continue
		}

		if matched {
			return true
		}
	}
	return false
}

// Values returns the variable Values to be redacted, given a
// redaction config string and an environment map.
func Values(logger shell.Logger, patterns []string, environment []env.Pair) []string {
	vars := Vars(logger, patterns, environment)
	if len(vars) == 0 {
		return nil
	}

	vals := make([]string, 0, len(vars))
	for _, pair := range vars {
		vals = append(vals, pair.Value)
	}

	return vals
}

// Vars returns the variable names and values to be redacted, given a
// redaction config string and an environment map.
func Vars(logger shell.Logger, patterns []string, environment []env.Pair) []env.Pair {
	var vars []env.Pair
	shortVars := make(map[string]struct{})

	for _, pair := range environment {
		// Does the name match any of the patterns?
		if !Match(logger, patterns, pair.Name) {
			continue
		}

		// The name matched, now test the length of the value.
		if len(pair.Value) < LengthMin {
			if len(pair.Value) > 0 {
				shortVars[pair.Name] = struct{}{}
			}
			continue
		}

		vars = append(vars, pair)
	}

	if len(shortVars) > 0 {
		// TODO: Use stdlib maps when it gets a Keys function (Go 1.22?)
		vars := maps.Keys(shortVars)
		sort.Strings(vars)
		logger.Warningf("Some variables have values below minimum length (%d bytes) and will not be redacted: %s", LengthMin, strings.Join(vars, ", "))
	}
	return vars
}
