// Package redact provides functions for determining values to redact.
package redact

import (
	"fmt"
	"io"
	"maps"
	"os"
	"path"
	"slices"
	"strings"

	"github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/internal/replacer"
)

// LengthMin is the shortest string length that will be considered a
// potential secret by the environment redactor. e.g. if the redactor is
// configured to filter out environment variables matching *_TOKEN, and
// API_TOKEN is set to "none", this minimum length will prevent the word "none"
// from being redacted from useful log output.
const LengthMin = 6

// Redacted ignores its input and returns "[REDACTED]".
func Redacted([]byte) []byte { return []byte("[REDACTED]") }

// String is a convenience wrapper for redacting small strings.
// This is fine to call repeatedly with many separate strings, but avoid using
// this to redact large streams - it requires buffering the whole input and
// output.
func String(input string, needles []string) string {
	var sb strings.Builder
	// strings.Builder.Write doesn't return an error, so neither should a
	// Replacer that writes to it. If there is a surprise error, that's panic
	// territory.
	repl := New(&sb, needles)
	if _, err := repl.Write([]byte(input)); err != nil {
		panic("Replacer failed to write to strings.Builder?")
	}
	if err := repl.Flush(); err != nil {
		panic("Replacer failed to flush to strings.Builder?")
	}
	return sb.String()
}

// NeedlesFromEnv matches the patterns against [os.Environ]. It returns values
// to redact and the names of env vars with "short" values.
func NeedlesFromEnv(patterns []string) (values, short []string, err error) {
	environ := env.FromSlice(os.Environ()).DumpPairs()
	toRedact, short, err := Vars(patterns, environ)
	if err != nil {
		return nil, nil, fmt.Errorf("finding env vars to redact: %w", err)
	}
	// Make the values unique.
	needles := make(map[string]struct{})
	for _, v := range toRedact {
		needles[v.Value] = struct{}{}
	}
	return slices.Collect(maps.Keys(needles)), short, nil
}

// New returns a replacer configured to write to dst, and redact all needles.
func New(dst io.Writer, needles []string) *replacer.Replacer {
	return replacer.New(dst, needles, Redacted)
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
