package redact

import (
	"testing"

	"github.com/buildkite/agent/v3/env"
	"github.com/google/go-cmp/cmp"
)

func TestVars(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		redactConfig []string
		environment  []env.Pair
		wantMatched  []env.Pair
		wantShort    []string
	}{
		{
			name:         "hunter2",
			redactConfig: []string{"*_PASSWORD", "*_TOKEN"},
			environment: []env.Pair{
				{Name: "BUILDKITE_PIPELINE", Value: "unit-test"},
				// These are example values, and are not leaked credentials
				{Name: "DATABASE_USERNAME", Value: "AzureDiamond"},
				{Name: "DATABASE_PASSWORD", Value: "hunter2"},
			},
			wantMatched: []env.Pair{{Name: "DATABASE_PASSWORD", Value: "hunter2"}},
			wantShort:   nil,
		},
		{
			name:         "short",
			redactConfig: []string{"*_PASSWORD", "*_TOKEN"},
			environment: []env.Pair{
				{Name: "BUILDKITE_PIPELINE", Value: "unit-test"},
				// These are example values, and are not leaked credentials
				{Name: "DATABASE_USERNAME", Value: "AzureDiamond"},
				{Name: "DATABASE_PASSWORD", Value: "hunt"},
			},
			wantMatched: nil,
			wantShort:   []string{"DATABASE_PASSWORD"},
		},
		{
			name:         "empty",
			redactConfig: nil,
			environment: []env.Pair{
				{Name: "FOO", Value: "BAR"},
				{Name: "BUILDKITE_PIPELINE", Value: "unit-test"},
			},
			wantMatched: nil,
			wantShort:   nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			matched, short, err := Vars(test.redactConfig, test.environment)
			if err != nil {
				t.Fatalf("Vars(%q, %q) error = %v", test.redactConfig, test.environment, err)
			}
			if diff := cmp.Diff(matched, test.wantMatched); diff != "" {
				t.Errorf("Vars(%q, %q) matched diff (-got +want)\n%s", test.redactConfig, test.environment, diff)
			}
			if diff := cmp.Diff(short, test.wantShort); diff != "" {
				t.Errorf("Vars(%q, %q) short diff (-got +want)\n%s", test.redactConfig, test.environment, diff)
			}
		})
	}
}

func TestRedactString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		needles []string
		input   string
		want    string
	}{
		{
			name:    "no needles",
			needles: nil,
			input:   "secret 1 secret 2 secret 3 s",
			want:    "secret 1 secret 2 secret 3 s",
		},
		{
			name:    "one needle",
			needles: []string{"secret 2"},
			input:   "secret 1 secret 2 secret 3 s",
			want:    "secret 1 [REDACTED] secret 3 s",
		},
		{
			name:    "three needles",
			needles: []string{"secret 1", "secret 2", "secret 3"},
			input:   "secret 1 secret 2 secret 3 s",
			want:    "[REDACTED] [REDACTED] [REDACTED] s",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := String(test.input, test.needles); got != test.want {
				t.Errorf("String(%q, %q) = %q, want %q", test.input, test.needles, got, test.want)
			}
		})
	}
}
