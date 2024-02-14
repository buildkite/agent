package clicommand

import (
	"errors"
	"strings"
	"testing"
)

func TestParseGitCredentialInput(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		lines       []string
		expected    string
		expectedErr error
	}{
		{
			name: "happy path",
			lines: []string{
				"protocol=https",
				"host=github.com",
				"path=buildkite/agent",
			},
			expected: "https://github.com/buildkite/agent",
		},
		{
			name: "missing protocol",
			lines: []string{
				"host=github.com",
				"path=buildkite/agent",
			},
			expectedErr: errMissingComponent,
		},
		{
			name: "missing host",
			lines: []string{
				"protocol=https",
				"path=buildkite/agent",
			},
			expectedErr: errMissingComponent,
		},
		{
			name: "missing path",
			lines: []string{
				"protocol=https",
				"host=github.com",
			},
			expectedErr: errMissingComponent,
		},
		{
			name: "non-https protocol",
			lines: []string{
				"protocol=ssh",
				"host=github.com",
				"path=buildkite/agent",
			},
			expectedErr: errNotHTTPS,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			input := strings.Join(tc.lines, "\n")
			actual, actualErr := parseGitURLFromCredentialInput(input)
			if !errors.Is(actualErr, tc.expectedErr) {
				t.Fatalf("parseGitURLFromCredentialInput(%q) = error(%q), want error(%q)", input, actualErr, tc.expectedErr)
			}

			if actual != tc.expected {
				t.Fatalf("parseGitURLFromCredentialInput(%q) = %q, want %q", input, actual, tc.expected)
			}
		})
	}
}
