package job

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/buildkite/agent/v3/job/shell"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseGittableURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		url, wantParsed, wantHost string
	}{
		{
			url:        "/home/vagrant/repo",
			wantParsed: "file:///home/vagrant/repo",
			wantHost:   "",
		},
		{
			url:        "file:///C:/Users/vagrant/repo",
			wantParsed: "file:///C:/Users/vagrant/repo",
			wantHost:   "",
		},
		{
			url:        "git@github.com:buildkite/agent.git",
			wantParsed: "ssh://git@github.com/buildkite/agent.git",
			wantHost:   "github.com",
		},
		{
			url:        "git@github.com-alias1:buildkite/agent.git",
			wantParsed: "ssh://git@github.com-alias1/buildkite/agent.git",
			wantHost:   "github.com-alias1",
		},
		{
			url:        "ssh://git@scm.xxx:7999/yyy/zzz.git",
			wantParsed: "ssh://git@scm.xxx:7999/yyy/zzz.git",
			wantHost:   "scm.xxx:7999",
		},
		{
			url:        "ssh://root@git.host.de:4019/var/cache/git/project.git",
			wantParsed: "ssh://root@git.host.de:4019/var/cache/git/project.git",
			wantHost:   "git.host.de:4019",
		},
	}

	for _, test := range tests {
		u, err := parseGittableURL(test.url)
		if err != nil {
			t.Errorf("parseGittableURL(%q) error = %v", test.url, err)
			continue
		}
		if got, want := u.String(), test.wantParsed; got != want {
			t.Errorf("parseGittableURL(%q) u.String() = %q, want %q", test.url, got, want)
		}
		if got, want := u.Host, test.wantHost; got != want {
			t.Errorf("parseGittableURL(%q) u.Host = %q, want %q", test.url, got, want)
		}
	}
}

func TestHostFromSSHG(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input   string
		want    string
		wantErr error
	}{
		{
			input: "hostname github.com\nport 22\nuser buildkite\naddressfamily any",
			want:  "github.com",
		},
		{
			input: "\nuser buildkite\naddressfamily any\nhostname blargh-no-alias.com\nport 22\n",
			want:  "blargh-no-alias.com",
		},
		{
			input: "hostname rad-git-host.com\nport 443\nuser cool-admin\naddressfamily any",
			want:  "rad-git-host.com:443",
		},
		{
			input:   "",
			wantErr: errNoHostname,
		},
		{
			input: `unknown option -- G
usage: ssh [-1246AaCfgKkMNnqsTtVvXxYy] [-b bind_address] [-c cipher_spec]
	[-D [bind_address:]port] [-E log_file] [-e escape_char]
	[-F configfile] [-I pkcs11] [-i identity_file]
	[-L [bind_address:]port:host:hostport] [-l login_name] [-m mac_spec]
	[-O ctl_cmd] [-o option] [-p port]
	[-Q cipher | cipher-auth | mac | kex | key]
	[-R [bind_address:]port:host:hostport] [-S ctl_path] [-W host:port]
	[-w local_tun[:remote_tun]] [user@]hostname [command]`,
			wantErr: errNoHostname,
		},
	}

	for _, test := range tests {
		got, err := hostFromSSHG(test.input)
		if !errors.Is(err, test.wantErr) {
			t.Errorf("hostFromSSHG(%q) error = %v, want %v", test.input, err, test.wantErr)
		}
		if got != test.want {
			t.Errorf("hostFromSSHG(%q) = %q, want %q", test.input, got, test.want)
		}
	}
}

func TestResolvingGitHostAliasesWithFlagSupport(t *testing.T) {
	t.Parallel()

	if _, err := os.Stat("/.dockerenv"); err != nil {
		t.Skip("TestResolvingGitHostAliasesWithFlagSupport only meaningful in the prepared Docker container")
	}

	// Use the real SSH bundled in the Go Docker image, with the config
	// .buildkite/build/ssh.conf.

	ctx := context.Background()

	sh := shell.NewTestShell(t)
	sh.Env.Set("PATH", os.Getenv("PATH"))

	tests := []struct {
		alias, want string
	}{
		{alias: "github.com-alias1", want: "github.com"},
		{alias: "blargh-no-alias.com", want: "blargh-no-alias.com"},
		{alias: "cool-alias", want: "rad-git-host.com:443"},
	}

	for _, test := range tests {
		if got := resolveGitHost(ctx, sh, test.alias); got != test.want {
			t.Errorf("resolveGitHost(ctx, sh, %q) = %q, want %q", test.alias, got, test.want)
		}
	}
}

func TestGitCheckRefFormat(t *testing.T) {
	for ref, want := range map[string]bool{
		"hello":          true,
		"hello-world":    true,
		"hello/world":    true,
		"--option":       false,
		" leadingspace":  false,
		"has space":      false,
		"has~tilde":      false,
		"has^caret":      false,
		"has:colon":      false,
		"has\007control": false,
		"has\177del":     false,
		"endswithdot.":   false,
		"two..dots":      false,
		"@":              false,
		"back\\slash":    false,
	} {
		if got := gitCheckRefFormat(ref); got != want {
			t.Errorf("gitCheckRefFormat(%q) = %t, want %t", ref, got, want)
		}
	}
}

func TestGitCheckoutValidatesRef(t *testing.T) {
	sh := new(mockShellRunner)
	defer sh.Check(t)
	err := gitCheckout(context.Background(), &shell.Shell{}, "", "--nope")
	assert.EqualError(t, err, `"--nope" is not a valid git ref format`)
}

func TestGitCheckout(t *testing.T) {
	sh := new(mockShellRunner).Expect("git", "checkout", "-f", "-q", "main")
	defer sh.Check(t)
	err := gitCheckout(context.Background(), sh, "-f -q", "main")
	require.NoError(t, err)
}

func TestGitCheckoutSketchyArgs(t *testing.T) {
	sh := new(mockShellRunner)
	defer sh.Check(t)
	err := gitCheckout(context.Background(), sh, "-f -q", "  --hello")
	assert.EqualError(t, err, `"  --hello" is not a valid git ref format`)
}

func TestGitClone(t *testing.T) {
	sh := new(mockShellRunner).Expect("git", "clone", "-v", "--references", "url", "--", "repo", "dir")
	defer sh.Check(t)
	err := gitClone(context.Background(), sh, "-v --references url", "repo", "dir")
	require.NoError(t, err)
}

func TestGitClean(t *testing.T) {
	sh := new(mockShellRunner).Expect("git", "clean", "--foo", "--bar")
	defer sh.Check(t)
	err := gitClean(context.Background(), sh, "--foo --bar")
	require.NoError(t, err)
}

func TestGitCleanSubmodules(t *testing.T) {
	sh := new(mockShellRunner).Expect("git", "submodule", "foreach", "--recursive", "git clean --foo --bar")
	defer sh.Check(t)
	err := gitCleanSubmodules(context.Background(), sh, "--foo --bar")
	require.NoError(t, err)
}

func TestGitFetch(t *testing.T) {
	sh := new(mockShellRunner).Expect("git", "fetch", "--foo", "--bar", "--", "repo", "ref1", "ref2")
	defer sh.Check(t)
	err := gitFetch(context.Background(), sh, "--foo --bar", "repo", "ref1", "ref2")
	require.NoError(t, err)
}

// mockShellRunner implements shellRunner for testing expected calls.
type mockShellRunner struct {
	got, want [][]string
}

func (r *mockShellRunner) Expect(cmd string, args ...string) *mockShellRunner {
	r.want = append(r.want, append([]string{cmd}, args...))
	return r
}

func (r *mockShellRunner) Run(_ context.Context, cmd string, args ...string) error {
	r.got = append(r.got, append([]string{cmd}, args...))
	return nil
}

func (r *mockShellRunner) Check(t *testing.T) {
	if diff := cmp.Diff(r.got, r.want); diff != "" {
		t.Errorf("mockShellRunner diff (-got +want):\n%s", diff)
	}
}
