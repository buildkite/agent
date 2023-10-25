// Package redact provides functions for determining values to redact.
package redact

import (
	"path"
	"sort"
	"strings"

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

// Values returns the variable Values to be redacted, given a
// redaction config string and an environment map.
func Values(logger shell.Logger, patterns []string, environment map[string]string) []string {
	vars := Vars(logger, patterns, environment)
	if len(vars) == 0 {
		return nil
	}

	vals := make([]string, 0, len(vars))
	for _, val := range vars {
		vals = append(vals, val)
	}

	return vals
}

// Vars returns the variable names and values to be redacted, given a
// redaction config string and an environment map.
func Vars(logger shell.Logger, patterns []string, environment map[string]string) map[string]string {
	// Lifted out of Bootstrap.setupRedactors to facilitate testing
	vars := make(map[string]string)
	shortVars := make(map[string]struct{})

	for name, val := range environment {
		for _, pattern := range patterns {
			matched, err := path.Match(pattern, name)
			if err != nil {
				// path.ErrBadPattern is the only error returned by path.Match
				logger.Warningf("Bad redacted vars pattern: %s", pattern)
				continue
			}

			if !matched {
				continue
			}
			if len(val) < LengthMin {
				if len(val) > 0 {
					shortVars[name] = struct{}{}
				}
				continue
			}

			vars[name] = val
			break // Break pattern loop, continue to next env var
		}
	}

	if len(shortVars) > 0 {
		// TODO: Use stdlib maps when it gets a Keys function (Go 1.22?)
		vars := maps.Keys(shortVars)
		sort.Strings(vars)
		logger.Warningf("Some variables have values below minimum length (%d bytes) and will not be redacted: %s", LengthMin, strings.Join(vars, ", "))
	}
	return vars
}
