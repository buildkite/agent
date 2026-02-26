package clicommand_test

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/clicommand"
	"github.com/buildkite/agent/v3/logger"
	"github.com/google/go-cmp/cmp"
)

func TestParseSecrets(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name            string
		inputData       string
		formatString    string
		applyVarsFilter bool
		wantSecrets     []string
	}{
		{
			name:         "json",
			inputData:    `{"hello": "world", "password": "hunter2"}`,
			formatString: clicommand.FormatStringJSON,
			wantSecrets:  []string{"world", "hunter2"},
		},
		{
			name:         "plaintext",
			inputData:    "hunter2\n",
			formatString: clicommand.FormatStringNone,
			wantSecrets:  []string{"hunter2"},
		},

		{
			name:            "vars filter",
			inputData:       `{"HELLO": "1", "MY_PASSWORD": "hunter2"}`,
			applyVarsFilter: true,
			formatString:    clicommand.FormatStringJSON,
			wantSecrets:     []string{"hunter2"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			input := strings.NewReader(tc.inputData)
			secrets, err := clicommand.ParseSecrets(
				logger.Discard,
				clicommand.RedactorAddConfig{
					Format:          tc.formatString,
					ApplyVarsFilter: tc.applyVarsFilter,
					RedactedVars:    *clicommand.RedactedVars.Value,
				},
				input,
			)
			if err != nil {
				t.Errorf("clicommand.ParseSecrets(logger, cfg, %q) error = %v", input, err)
			}

			slices.Sort(secrets)
			slices.Sort(tc.wantSecrets)
			if diff := cmp.Diff(secrets, tc.wantSecrets); diff != "" {
				t.Errorf("clicommand.ParseSecrets(logger, cfg, %q) secrets diff (-got +want):\n%s", input, diff)
			}
		})
	}
}

func TestParseSecrets_JSONErrors(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		inputData string
		wantError any
	}{
		{
			name:      "type mismatch",
			inputData: `{"hello": 1, "password": "hunter2"}`,
			wantError: new(*json.UnmarshalTypeError),
		},
		{
			name:      "syntax error",
			inputData: `}}{"hello": , "pas: "hun'}`,
			wantError: new(*json.SyntaxError),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			input := strings.NewReader(tc.inputData)

			_, err := clicommand.ParseSecrets(
				logger.Discard,
				clicommand.RedactorAddConfig{
					Format: clicommand.FormatStringJSON,
				},
				input,
			)
			if !errors.As(err, tc.wantError) {
				t.Errorf("clicommand.ParseSecrets(logger, cfg, %q) error = %v, want error wrapping %T", input, err, tc.wantError)
			}
		})
	}
}
