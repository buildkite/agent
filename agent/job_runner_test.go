package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/logger"
)

func TestTruncateEnv(t *testing.T) {
	l := logger.NewBuffer()
	key := "FOO"
	env := map[string]string{key: strings.Repeat("a", 100)}
	limit := 64
	if err := truncateEnv(l, env, key, limit); err != nil {
		t.Fatalf("truncateEnv(logger, %v, %q, %d) = %v", env, key, limit, err)
	}
	if got, want := env["FOO"], "aaaaaaaaaaaaaaaaaaaaaaaaaa[value truncated 100 -> 59 bytes]"; got != want {
		t.Errorf("after truncateEnv(logger, %v, %q, %d): env[%q] = %q, want %q", env, key, limit, key, got, want)
	}
	format := "FOO=%s\000"
	if got, want := len(fmt.Sprintf(format, env["FOO"])), limit; got != want {
		t.Errorf("after truncateEnv(logger, %v, %q, %d): len(fmt.Sprintf(%q, env[%q])) = %d, want %d", env, key, limit, format, key, got, want)
	}
}

func TestValidateJobValue(t *testing.T) {
	bkTarget := "github.com/buildkite/test"
	bkTargetRE := regexp.MustCompile(`^github\.com/buildkite/.*`)
	ghTargetRE := regexp.MustCompile(`^github\.com/nope/.*`)

	tests := []struct {
		name           string
		allowedTargets []*regexp.Regexp
		pipelineTarget string
		wantErr        bool
	}{
		{
			name:           "No error. Allowed targets no configured.",
			allowedTargets: []*regexp.Regexp{},
			pipelineTarget: bkTarget,
		}, {
			name:           "No pipeline target match",
			allowedTargets: []*regexp.Regexp{ghTargetRE},
			pipelineTarget: bkTarget,
			wantErr:        true,
		}, {
			name:           "Pipeline target match",
			allowedTargets: []*regexp.Regexp{ghTargetRE, bkTargetRE},
			pipelineTarget: bkTarget,
		},
	}

	for _, tc := range tests {
		err := validateJobValue(tc.allowedTargets, tc.pipelineTarget)
		if (err != nil) != tc.wantErr {
			t.Errorf("validateJobValue() error = %v, wantErr = %v", err, tc.wantErr)
		}
	}
}

func TestJobTimeoutFilePath(t *testing.T) {
	t.Parallel()

	got := jobTimeoutFilePath("abc123", false)
	want := filepath.Join(os.TempDir(), "job-timeout-abc123")
	if got != want {
		t.Errorf("jobTimeoutFilePath(%q, false) = %q, want %q", "abc123", got, want)
	}

	if got, want := jobTimeoutFilePath("abc123", true), filepath.Join("/workspace", "job-timeout-abc123"); got != want {
		t.Errorf("jobTimeoutFilePath(%q, true) = %q, want %q", "abc123", got, want)
	}
}

func TestCancelReasonString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		reason CancelReason
		want   string
	}{
		{CancelReasonJobState, "job cancelled on Buildkite"},
		{CancelReasonAgentStopping, "agent is stopping"},
		{CancelReasonInvalidToken, "access token is invalid"},
		{CancelReasonJobTimeout, "job timed out on Buildkite"},
		{CancelReason(99), "unknown"},
	}

	for _, tc := range tests {
		if got := tc.reason.String(); got != tc.want {
			t.Errorf("CancelReason(%d).String() = %q, want %q", tc.reason, got, tc.want)
		}
	}
}

func TestValidEnvShellKey(t *testing.T) {
	cases := []struct {
		key  string
		want bool
	}{
		{"FOO", true},
		{"_FOO", true},
		{"FOO_BAR_123", true},
		{"foo", true},
		{"BUILDKITE_MESSAGE", true},
		{"", false},
		{"1FOO", false},
		{"FOO=BAR", false},
		{"FOO BAR", false},
		{"FOO\nBAR", false},
		{"FOO$(cmd)", false},
		{"FOO`cmd`", false},
		{"FOO-BAR", false},
		{"FOO.BAR", false},
	}
	for _, tc := range cases {
		if got := validEnvShellKey(tc.key); got != tc.want {
			t.Errorf("validEnvShellKey(%q) = %v, want %v", tc.key, got, tc.want)
		}
	}
}

func TestPosixShellQuote(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", `''`},
		{"abc", `'abc'`},
		{"hello world", `'hello world'`},
		{"$(echo PWNED)", `'$(echo PWNED)'`},
		{"`echo PWNED`", "'`echo PWNED`'"},
		{"${HOME}", `'${HOME}'`},
		{"$HOME", `'$HOME'`},
		{"it's", `'it'\''s'`},
		{"'$(evil)'", `''\''$(evil)'\'''`},
		{"line1\nline2", "'line1\nline2'"},
	}
	for _, tc := range cases {
		if got := posixShellQuote(tc.in); got != tc.want {
			t.Errorf("posixShellQuote(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestPosixShellQuote_RoundTripBash verifies that values quoted by
// posixShellQuote round-trip through `bash -c 'source FILE; printf %s "$VAR"'`
// to exactly the original bytes, even when they contain shell metacharacters.
// This is the property that makes the env-file safe to source.
func TestPosixShellQuote_RoundTripBash(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available on PATH; skipping round-trip test")
	}

	values := []string{
		"plain",
		"with spaces",
		"$(echo PWNED > /tmp/should_not_exist)",
		"`echo PWNED > /tmp/should_not_exist`",
		"${IFS}",
		"$HOME",
		"it's a value",
		"'$(evil)'",
		"newline\nhere",
		"tab\there",
		"backslash\\here",
		"double\"quote",
		"semi;colon && other",
		`mix '"$(echo X)" 'and stuff`,
	}

	dir := t.TempDir()
	for i, v := range values {
		envFile := filepath.Join(dir, fmt.Sprintf("env_%d", i))
		line := fmt.Sprintf("VAR=%s\n", posixShellQuote(v))
		if err := os.WriteFile(envFile, []byte(line), 0o600); err != nil {
			t.Fatalf("writing env file: %v", err)
		}
		// Source the file in a subshell and print the value verbatim.
		// If quoting were broken, command substitution would run and
		// either alter the output or create the canary file.
		script := fmt.Sprintf(`set -u; source %q; printf %%s "$VAR"`, envFile)
		out, err := exec.Command(bash, "-c", script).Output()
		if err != nil {
			t.Errorf("bash sourcing failed for value %q (line=%q): %v", v, line, err)
			continue
		}
		if string(out) != v {
			t.Errorf("round-trip mismatch:\n  input:  %q\n  line:   %q\n  output: %q", v, line, string(out))
		}
	}
	// Sanity check: no command substitution side-effect ran.
	if _, err := os.Stat("/tmp/should_not_exist"); err == nil {
		t.Errorf("command substitution executed: /tmp/should_not_exist was created")
		_ = os.Remove("/tmp/should_not_exist")
	}
}
