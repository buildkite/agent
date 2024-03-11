package clicommand_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/clicommand"
	"github.com/buildkite/agent/v3/logger"
	"gotest.tools/v3/assert"
)

func TestParseSecrets(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name              string
		inputData         string
		formatString      string
		expectedSecrets   []string
		errorTextContains string
	}{
		{
			name:            "json",
			inputData:       `{"hello": "world", "password": "hunter2"}`,
			formatString:    clicommand.FormatStringJSON,
			expectedSecrets: []string{"world", "hunter2"},
		},
		{
			name:            "plaintext",
			inputData:       "hunter2\n",
			formatString:    clicommand.FormatStringNone,
			expectedSecrets: []string{"hunter2"},
		},
		{
			name:              "invalid_json",
			inputData:         `{"hello": 1, "password": "hunter2"}`,
			formatString:      clicommand.FormatStringJSON,
			errorTextContains: "failed to parse as string valued JSON",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			input := strings.NewReader(tc.inputData)
			secrets, err := clicommand.ParseSecrets(logger.Discard, clicommand.RedactorAddConfig{Format: tc.formatString}, input)
			if tc.errorTextContains != "" {
				assert.ErrorContains(t, err, tc.errorTextContains)
				return
			}
			assert.NilError(t, err)

			slices.Sort(secrets)
			slices.Sort(tc.expectedSecrets)
			assert.DeepEqual(t, secrets, tc.expectedSecrets)
		})
	}
}
