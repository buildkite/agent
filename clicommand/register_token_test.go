package clicommand

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestIsIndirectToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		token string
		want  bool
	}{
		{token: "bkt_llamas", want: false},
		{token: "", want: false},
		{token: "file:///etc/buildkite-agent/token", want: true},
		{token: "fd://3", want: true},
		{token: "fdish-token", want: false},
	}

	for _, test := range tests {
		if got, want := isIndirectToken(test.token), test.want; got != want {
			t.Errorf("isIndirectToken(%q) = %t, want %t", test.token, got, want)
		}
	}
}

func TestRegistrationTokenFromArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		args      []string
		wantToken string
		wantFound bool
	}{
		{
			name: "no token flag",
			args: []string{"buildkite-agent", "start", "--spawn", "2"},
		},
		{
			name:      "space separated",
			args:      []string{"buildkite-agent", "start", "--token", "llamas"},
			wantToken: "llamas",
			wantFound: true,
		},
		{
			name:      "equals separated",
			args:      []string{"buildkite-agent", "start", "--token=llamas"},
			wantToken: "llamas",
			wantFound: true,
		},
		{
			name:      "single dash",
			args:      []string{"buildkite-agent", "start", "-token", "llamas"},
			wantToken: "llamas",
			wantFound: true,
		},
		{
			name:      "single dash equals",
			args:      []string{"buildkite-agent", "start", "-token=alpacas"},
			wantToken: "alpacas",
			wantFound: true,
		},
		{
			name:      "last occurrence wins",
			args:      []string{"buildkite-agent", "start", "--token", "llamas", "--token=alpacas"},
			wantToken: "alpacas",
			wantFound: true,
		},
		{
			name: "trailing flag with no value",
			args: []string{"buildkite-agent", "start", "--token"},
		},
		{
			name:      "empty value",
			args:      []string{"buildkite-agent", "start", "--token", ""},
			wantToken: "",
			wantFound: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			token, found := registrationTokenFromArgs(test.args)
			if got, want := token, test.wantToken; got != want {
				t.Errorf("registrationTokenFromArgs(%q) token = %q, want %q", test.args, got, want)
			}
			if got, want := found, test.wantFound; got != want {
				t.Errorf("registrationTokenFromArgs(%q) found = %t, want %t", test.args, got, want)
			}
		})
	}
}

func TestReplaceTokenInArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "space separated",
			args: []string{"buildkite-agent", "start", "--token", "llamas", "--spawn", "2"},
			want: []string{"buildkite-agent", "start", "--token", "fd://3", "--spawn", "2"},
		},
		{
			name: "equals separated",
			args: []string{"buildkite-agent", "start", "--token=llamas"},
			want: []string{"buildkite-agent", "start", "--token=fd://3"},
		},
		{
			name: "single dash",
			args: []string{"buildkite-agent", "start", "-token", "llamas", "-token=alpacas"},
			want: []string{"buildkite-agent", "start", "-token", "fd://3", "-token=fd://3"},
		},
		{
			name: "no token flag",
			args: []string{"buildkite-agent", "start"},
			want: []string{"buildkite-agent", "start"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := replaceTokenInArgs(test.args, "fd://3")
			if diff := cmp.Diff(got, test.want); diff != "" {
				t.Errorf("replaceTokenInArgs(%q, fd://3) diff (-got +want):\n%s", test.args, diff)
			}
		})
	}
}

func TestScrubTokenFromEnviron(t *testing.T) {
	t.Parallel()

	environ := []string{
		"HOME=/home/llama",
		"BUILDKITE_AGENT_TOKEN=secret",
		"BUILDKITE_AGENT_TOKENISH=not-scrubbed",
		"PATH=/usr/bin",
	}
	want := []string{
		"HOME=/home/llama",
		"BUILDKITE_AGENT_TOKENISH=not-scrubbed",
		"PATH=/usr/bin",
	}
	if diff := cmp.Diff(scrubTokenFromEnviron(environ), want); diff != "" {
		t.Errorf("scrubTokenFromEnviron diff (-got +want):\n%s", diff)
	}
}

func TestResolveRegistrationToken_Plain(t *testing.T) {
	t.Parallel()

	got, err := resolveRegistrationToken("bkt_llamas")
	if err != nil {
		t.Fatalf("resolveRegistrationToken(bkt_llamas) error = %v", err)
	}
	if want := "bkt_llamas"; got != want {
		t.Errorf("resolveRegistrationToken(bkt_llamas) = %q, want %q", got, want)
	}
}

func TestResolveRegistrationToken_File(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("bkt_llamas\n"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) = %v", path, err)
	}

	got, err := resolveRegistrationToken("file://" + path)
	if err != nil {
		t.Fatalf("resolveRegistrationToken(file://%s) error = %v", path, err)
	}
	if want := "bkt_llamas"; got != want {
		t.Errorf("resolveRegistrationToken(file://%s) = %q, want %q", path, got, want)
	}
}

func TestResolveRegistrationToken_FileMissing(t *testing.T) {
	t.Parallel()

	if _, err := resolveRegistrationToken("file:///nonexistent/token"); err == nil {
		t.Error("resolveRegistrationToken(file:///nonexistent/token) error = nil, want error")
	}
}

func TestResolveRegistrationToken_FD(t *testing.T) {
	t.Parallel()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() = %v", err)
	}
	if _, err := w.WriteString("bkt_llamas"); err != nil {
		t.Fatalf("w.WriteString = %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("w.Close() = %v", err)
	}

	got, err := resolveRegistrationToken("fd://" + strconv.FormatUint(uint64(r.Fd()), 10))
	if err != nil {
		t.Fatalf("resolveRegistrationToken(fd://) error = %v", err)
	}
	if want := "bkt_llamas"; got != want {
		t.Errorf("resolveRegistrationToken(fd://) = %q, want %q", got, want)
	}
}

func TestResolveRegistrationToken_BadFD(t *testing.T) {
	t.Parallel()

	if _, err := resolveRegistrationToken("fd://not-a-number"); err == nil {
		t.Error("resolveRegistrationToken(fd://not-a-number) error = nil, want error")
	}
}
