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
		{
			name:    "needle with newline in two forms",
			needles: []string{"secret\n1"},
			input:   "secret\n1 secret\\n1 s",
			want:    "[REDACTED] [REDACTED] s",
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

// TestURLCredentials asserts that an embedded password is masked while URLs
// without a secret, ssh:// SSH remotes and relative submodule paths pass
// through unchanged.
func TestURLCredentials(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "https with creds", in: "https://x-access-token:ghs_secret@github.com/org/repo.git", want: "https://x-access-token:xxxxx@github.com/org/repo.git"},
		{name: "https with creds but no user", in: "https://:ghs_secret@github.com/org/repo.git", want: "https://:xxxxx@github.com/org/repo.git"},
		{name: "https no creds", in: "https://github.com/org/repo.git", want: "https://github.com/org/repo.git"},
		{name: "https user only", in: "https://user@github.com/org/repo.git", want: "https://user@github.com/org/repo.git"},
		{name: "scp-style ssh", in: "git@github.com:org/repo.git", want: "git@github.com:org/repo.git"},
		{name: "ssh scheme", in: "ssh://git@github.com/org/repo.git", want: "ssh://git@github.com/org/repo.git"},
		{name: "relative submodule", in: "../relative/submodule", want: "../relative/submodule"},
		{name: "empty", in: "", want: ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := URLCredentials(test.in); got != test.want {
				t.Errorf("URLCredentials(%q) = %q, want %q", test.in, got, test.want)
			}
		})
	}
}
