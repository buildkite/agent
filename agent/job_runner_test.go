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
	t.Parallel()

	l := logger.NewBuffer()
	env := map[string]string{"FOO": strings.Repeat("a", 100)}
	err := truncateEnv(l, env, "FOO", 64)
	require.NoError(t, err)
	assert.Equal(t, "aaaaaaaaaaaaaaaaaaaaaaaaaa[value truncated 100 -> 59 bytes]", env["FOO"])
	assert.Equal(t, 64, len(fmt.Sprintf("FOO=%s\000", env["FOO"])))
}

func TestValidateJobValue(t *testing.T) {
	t.Parallel()

	bkTarget := "github.com/buildkite/test"
	bkTargetRe := regexp.MustCompile(`^github\.com/buildkite/.*`)
	ghTargetRe := regexp.MustCompile(`^github\.com/nope/.*`)

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
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := validateJobValue(tc.allowedTargets, tc.pipelineTarget)
			if (err != nil) != tc.wantErr {
				t.Errorf("validateJobValue() error = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}
