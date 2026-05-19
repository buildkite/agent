package clicommand_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/buildkite/agent/v4/clicommand"
	"github.com/buildkite/agent/v4/logger"
	"github.com/google/go-cmp/cmp"
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
				if want := tc.errorTextContains; err == nil || !strings.Contains(err.Error(), want) {
					t.Fatalf("clicommand.ParseSecrets(logger.Discard, clicommand.RedactorAddConfig{Format: tc.formatString}, input) error = %v, want error containing %q", err, want)
				}
				return
			}
			if err != nil {
				t.Fatalf("clicommand.ParseSecrets(logger.Discard, clicommand.RedactorAddConfig{Format: tc.formatString}, input) error = %v, want nil", err)
			}

			slices.Sort(secrets)
			slices.Sort(tc.expectedSecrets)
			if diff := cmp.Diff(tc.expectedSecrets, secrets); diff != "" {
				t.Fatalf("tc.expectedSecrets diff (-got +want):\n%s", diff)
			}
		})
	}
}
