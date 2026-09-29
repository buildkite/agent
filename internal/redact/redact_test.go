package redact

import (
	"strings"
	"testing"

	"github.com/buildkite/agent/v4/env"
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

func TestTokenPrefixes(t *testing.T) {
	t.Parallel()

	// These are made-up values in the shape of real tokens, and are not
	// leaked credentials. Prefixes and bodies are kept apart in the source so
	// that secret scanners (e.g. GitHub push protection) don't flag them.
	const (
		orgIDDotBase58 = "MTIzNDU.4hZfF3sRNuRUMZm5m9xGcH2xWCqzHzSHEeq6onKMDAtYKfsx2VqFGSapzAehThRABbT"
		orgIDHex       = "MTIzNDU_0123456789abcdef0123456789abcdef01234567"
	)
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "agent access token (org ID, dot, base58)",
			input: "token: " + "bkaa_" + orgIDDotBase58 + "\n",
			want:  "token: [REDACTED]\n",
		},
		{
			name:  "API token (40 hex)",
			input: "curl -H 'Authorization: Bearer bkua_0123456789abcdef0123456789abcdef01234567' https://api.buildkite.com\n",
			want:  "curl -H 'Authorization: Bearer [REDACTED]' https://api.buildkite.com\n",
		},
		{
			name:  "portal token (org ID, underscore, hex)",
			input: "bkpat_" + orgIDHex + "\n",
			want:  "[REDACTED]\n",
		},
		{
			name:  "job acquisition token (JWT) in ps output",
			input: "    1 ?        Ss     0:00 tini -- buildkite-agent bootstrap --acquire-job bkjat_eyJhbGciOiJIUzUxMiJ9.eyJqb2JfaWQiOiIwMTkwMDAwMC0wMDAwLTAwMDAtMDAwMC0wMDAwMDAwMDAwMDAiLCJleHAiOjE3MDAwMDAwMDB9.c2lnbmF0dXJlc2lnbmF0dXJlc2lnbmF0dXJlc2lnbmF0dXJlc2lnbmF0dXJlc2lnbmF0dXJlc2lnbmF0dXJlc2lnbmF0dXJl --job 01900000-0000-0000-0000-000000000000\n",
			want:  "    1 ?        Ss     0:00 tini -- buildkite-agent bootstrap --acquire-job [REDACTED] --job 01900000-0000-0000-0000-000000000000\n",
		},
		{
			name:  "truncated job acquisition token at end of line",
			input: "tini -- buildkite-agent bootstrap --acquire-job bkjat_eyJhbGciOiJIUzUxMiJ9.eyJqb2JfaWQ\n",
			want:  "tini -- buildkite-agent bootstrap --acquire-job [REDACTED]\n",
		},
		{
			name:  "token at end of input without newline",
			input: "bkct_" + orgIDDotBase58,
			want:  "[REDACTED]",
		},
		{
			name:  "token quoted with %q",
			input: `token="bkur_0123456789abcdef0123456789abcdef01234567"` + "\n",
			want:  `token="[REDACTED]"` + "\n",
		},
		{
			name:  "short placeholder is not redacted",
			input: "bkjat_encoded-token\n",
			want:  "bkjat_encoded-token\n",
		},
		{
			name:  "body one shorter than minimum is not redacted",
			input: "bkua_" + strings.Repeat("a", TokenBodyLengthMin-1) + "\n",
			want:  "bkua_" + strings.Repeat("a", TokenBodyLengthMin-1) + "\n",
		},
		{
			name:  "body exactly minimum is redacted",
			input: "bkua_" + strings.Repeat("a", TokenBodyLengthMin) + "\n",
			want:  "[REDACTED]\n",
		},
		{
			name:  "unknown prefix is not redacted",
			input: "bkzz_0123456789abcdef0123456789abcdef01234567\n",
			want:  "bkzz_0123456789abcdef0123456789abcdef01234567\n",
		},
		{
			name:  "prefix without underscore is not redacted",
			input: "bkua0123456789abcdef0123456789abcdef01234567\n",
			want:  "bkua0123456789abcdef0123456789abcdef01234567\n",
		},
		{
			name:  "env var names are not redacted",
			input: "BUILDKITE_AGENT_ACCESS_TOKEN=xyz BUILDKITE_JOB_ACQUISITION_TOKEN=xyz\n",
			want:  "BUILDKITE_AGENT_ACCESS_TOKEN=xyz BUILDKITE_JOB_ACQUISITION_TOKEN=xyz\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var sb strings.Builder
			r := New(&sb, nil)
			r.AddPrefixes(TokenPrefixes()...)
			if _, err := r.Write([]byte(test.input)); err != nil {
				t.Fatalf("r.Write(%q) error = %v", test.input, err)
			}
			if err := r.Flush(); err != nil {
				t.Fatalf("r.Flush() error = %v", err)
			}
			if got := sb.String(); got != test.want {
				t.Errorf("redacting %q got %q, want %q", test.input, got, test.want)
			}
		})
	}
}

func TestTokenPrefixesCoverEveryPrefix(t *testing.T) {
	t.Parallel()

	// Every known prefix followed by a minimum-length body is redacted.
	for _, prefix := range tokenPrefixes {
		t.Run(prefix, func(t *testing.T) {
			t.Parallel()

			input := "token: " + prefix + strings.Repeat("Aa0-_.", TokenBodyLengthMin/6+1) + " end\n"

			var sb strings.Builder
			r := New(&sb, nil)
			r.AddPrefixes(TokenPrefixes()...)
			if _, err := r.Write([]byte(input)); err != nil {
				t.Fatalf("r.Write(%q) error = %v", input, err)
			}
			if err := r.Flush(); err != nil {
				t.Fatalf("r.Flush() error = %v", err)
			}
			if got, want := sb.String(), "token: [REDACTED] end\n"; got != want {
				t.Errorf("redacting %q got %q, want %q", input, got, want)
			}
		})
	}
}

// TestURLCredentials asserts that all URL userinfo is masked, while URLs
// without userinfo, scp-style SSH remotes and relative paths pass through.
func TestURLCredentials(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "https with creds", in: "https://x-access-token:ghs_secret@github.com/org/repo.git", want: "https://xxxxx@github.com/org/repo.git"},
		{name: "https with creds but no user", in: "https://:ghs_secret@github.com/org/repo.git", want: "https://xxxxx@github.com/org/repo.git"},
		{name: "https no creds", in: "https://github.com/org/repo.git", want: "https://github.com/org/repo.git"},
		{name: "https username token", in: "https://test-token@github.com/org/repo.git", want: "https://xxxxx@github.com/org/repo.git"},
		{name: "username token with empty password", in: "https://test-token:@example.com/repo.git", want: "https://xxxxx@example.com/repo.git"},
		{name: "tokens in both fields", in: "https://test-token:test-password@example.com/repo.git", want: "https://xxxxx@example.com/repo.git"},
		{name: "encoded username", in: "https://test%40token%3Asecret@example.com:8443/org/repo%20name.git?ref=main#fragment", want: "https://xxxxx@example.com:8443/org/repo%20name.git?ref=main#fragment"},
		{name: "http username token", in: "http://test-token@example.com/repo.git", want: "http://xxxxx@example.com/repo.git"},
		{name: "scheme relative username token", in: "//test-token@example.com/repo.git", want: "//xxxxx@example.com/repo.git"},
		{name: "scp-style ssh", in: "git@github.com:org/repo.git", want: "git@github.com:org/repo.git"},
		{name: "ssh scheme", in: "ssh://git@github.com/org/repo.git", want: "ssh://xxxxx@github.com/org/repo.git"},
		{name: "relative submodule", in: "../relative/submodule", want: "../relative/submodule"},
		{name: "empty", in: "", want: ""},
		{name: "unparsable scheme url with creds", in: "https://x-access-token:ghs_secret@github.com/org/repo.git\x7f", want: "(invalid URL)"},
		{name: "unparsable schemeless ref", in: "git@github.com:org/repo.git\x7f", want: "git@github.com:org/repo.git\x7f"},
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
