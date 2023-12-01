package agent

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTruncateEnv(t *testing.T) {
	l := logger.NewBuffer()
	env := map[string]string{"FOO": strings.Repeat("a", 100)}
	err := truncateEnv(l, env, "FOO", 64)
	require.NoError(t, err)
	assert.Equal(t, "aaaaaaaaaaaaaaaaaaaaaaaaaa[value truncated 100 -> 59 bytes]", env["FOO"])
	assert.Equal(t, 64, len(fmt.Sprintf("FOO=%s\000", env["FOO"])))
}

func TestValidateJobValue(t *testing.T) {
	bkTarget := "github.com/buildkite/test"
	bkTargetRe := regexp.MustCompile("^github.com/buildkite/.*")
	ghTargetRe := regexp.MustCompile("^github.com/nope/.*")

	tests := []struct {
		name           string
		allowedTargets []*regexp.Regexp
		pipelineTarget string
		wantErr        bool
	}{{
		name:           "No error. Allowed targets no configured.",
		allowedTargets: []*regexp.Regexp{},
		pipelineTarget: bkTarget,
	}, {
		name:           "No pipeline target match",
		allowedTargets: []*regexp.Regexp{ghTargetRe},
		pipelineTarget: bkTarget,
		wantErr:        true,
	}, {
		name:           "Pipeline target match",
		allowedTargets: []*regexp.Regexp{ghTargetRe, bkTargetRe},
		pipelineTarget: bkTarget,
	}}

	for _, tc := range tests {
		err := validateJobValue(tc.allowedTargets, tc.pipelineTarget)
		if (err != nil) != tc.wantErr {
			t.Errorf("validateJobValue() error = %v, wantErr = %v", err, tc.wantErr)
		}
	}
}

func TestAllowedEnvironmentVariables(t *testing.T) {
	testCases := []struct {
		name                         string
		environmentVariables         map[string]string
		allowedEnvironmentVariables  []*regexp.Regexp
		expectedEnvironmentVariables map[string]string
		expectedDeniedVariables      []string
	}{{
		name: "Allow list without regex",
		environmentVariables: map[string]string{
			"CI":    "True",
			"SHELL": "/bin/sh",
		},
		allowedEnvironmentVariables: []*regexp.Regexp{
			regexp.MustCompile("CI"),
		},
		expectedEnvironmentVariables: map[string]string{
			"CI": "True",
		},
		expectedDeniedVariables: []string{"SHELL"},
	}, {
		name: "Allow list with regex (anchored)",
		environmentVariables: map[string]string{
			"CI":                 "True",
			"BUILDKITE_METADATA": "Some data...",
			"BUILDKITE_AGENT_ID": "SOME_AGENT",
			"SHELL":              "/bin/sh",
		},
		allowedEnvironmentVariables: []*regexp.Regexp{
			regexp.MustCompile("CI"),
			regexp.MustCompile("^BUILDKITE_.*$"),
		},
		expectedEnvironmentVariables: map[string]string{
			"CI":                 "True",
			"BUILDKITE_METADATA": "Some data...",
			"BUILDKITE_AGENT_ID": "SOME_AGENT",
		},
		expectedDeniedVariables: []string{"SHELL"},
	}, {
		name: "Allow list with regex (non-anchored)",
		environmentVariables: map[string]string{
			"CI":                 "True",
			"BUILDKITE_METADATA": "Some data...",
			"BUILDKITE_AGENT_ID": "SOME_AGENT",
			"SHELL":              "/bin/sh",
		},
		allowedEnvironmentVariables: []*regexp.Regexp{
			regexp.MustCompile("CI"),
			regexp.MustCompile("BUILDKITE_.*"),
		},
		expectedEnvironmentVariables: map[string]string{
			"CI":                 "True",
			"BUILDKITE_METADATA": "Some data...",
			"BUILDKITE_AGENT_ID": "SOME_AGENT",
		},
		expectedDeniedVariables: []string{"SHELL"},
	}}

	for _, testCase := range testCases {
		t.Logf("Running test case: %s", testCase.name)
		assert.ElementsMatch(t, testCase.expectedDeniedVariables, filterEnv(testCase.environmentVariables, testCase.allowedEnvironmentVariables))
		assert.Equal(t, testCase.expectedEnvironmentVariables, testCase.environmentVariables)
	}
}
