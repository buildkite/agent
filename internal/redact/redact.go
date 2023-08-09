// Package redact provides functions for determining values to redact.
package redact

import (
	"path"

	"github.com/buildkite/agent/v3/internal/job/shell"
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
					logger.Warningf("Value of %s below minimum length (%d bytes) and will not be redacted", name, LengthMin)
				}
				continue
			}

			vars[name] = val
			break // Break pattern loop, continue to next env var
		}
	}

	return vars
}
