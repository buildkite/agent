package job

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/buildkite/agent/v4/internal/shell"
	"github.com/google/go-cmp/cmp"
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
		t.Run(test.url, func(t *testing.T) {
			t.Parallel()
			u, err := parseGittableURL(test.url)
			if err != nil {
				t.Errorf("parseGittableURL(%q) error = %v", test.url, err)
				return
			}
			if got, want := u.String(), test.wantParsed; got != want {
				t.Errorf("parseGittableURL(%q) u.String() = %q, want %q", test.url, got, want)
			}
			if got, want := u.Host, test.wantHost; got != want {
				t.Errorf("parseGittableURL(%q) u.Host = %q, want %q", test.url, got, want)
			}
		})
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

	ctx := t.Context()

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
		t.Run(test.alias, func(t *testing.T) {
			t.Parallel()
			if got := resolveGitHost(ctx, sh, test.alias); got != test.want {
				t.Errorf("resolveGitHost(ctx, sh, %q) = %q, want %q", test.alias, got, test.want)
			}
		})
	}
}

func TestGitCheckRefFormat(t *testing.T) {
	t.Parallel()
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
		t.Run(ref, func(t *testing.T) {
			t.Parallel()
			if got := gitCheckRefFormat(ref); got != want {
				t.Errorf("gitCheckRefFormat(%q) = %t, want %t", ref, got, want)
			}
		})
	}
}

func TestGitCheckoutValidatesRef(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	sh := shell.NewTestShell(t, shell.WithDryRun(true))
	err := gitCheckout(ctx, sh, "", "--nope")
	if got, want := err.Error(), `"--nope" is not a valid git ref format`; got != want {
		t.Errorf(`gitCheckout(ctx, sh, "", "--nope") error = %q, want %q`, got, want)
	}
}

func TestGitCheckout(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	var gotLog [][]string
	sh := shell.NewTestShell(t, shell.WithDryRun(true), shell.WithCommandLog(&gotLog))

	absoluteGit, err := sh.AbsolutePath("git")
	if err != nil {
		t.Fatalf("sh.AbsolutePath(git) = %v", err)
	}

	if err := gitCheckout(ctx, sh, "-f -q", "main"); err != nil {
		t.Fatalf(`gitCheckout(ctx, sh, "-f -q", "main") = %v`, err)
	}

	wantLog := [][]string{{absoluteGit, "-c", "advice.detachedHead=false", "checkout", "-f", "-q", "main"}}
	if diff := cmp.Diff(gotLog, wantLog); diff != "" {
		t.Errorf("executed commands diff (-got +want):\n%s", diff)
	}
}

func TestGitCheckoutSketchyArgs(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	sh := shell.NewTestShell(t, shell.WithDryRun(true))
	err := gitCheckout(ctx, sh, "-f -q", "  --hello")
	if got, want := err.Error(), `"  --hello" is not a valid git ref format`; got != want {
		t.Errorf(`gitCheckout(ctx, sh, "-f -q", "  --hello") error = %q, want %q`, got, want)
	}
}

func TestGitClone(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	var gotLog [][]string
	sh := shell.NewTestShell(t, shell.WithDryRun(true), shell.WithCommandLog(&gotLog))

	absoluteGit, err := sh.AbsolutePath("git")
	if err != nil {
		t.Fatalf("sh.AbsolutePath(git) = %v", err)
	}

	if err := gitClone(ctx, sh,
		[]string{"-c", "credential.helper=/path with spaces/helper"},
		[]string{"-v", "--reference", "url"},
		"repo", "dir",
	); err != nil {
		t.Fatalf(`gitClone(ctx, sh, [-v --reference url], "repo", "dir") = %v`, err)
	}

	wantLog := [][]string{{
		absoluteGit,
		"-c", "credential.helper=/path with spaces/helper",
		"clone", "-v", "--reference", "url", "--", "repo", "dir",
	}}
	if diff := cmp.Diff(gotLog, wantLog); diff != "" {
		t.Errorf("executed commands diff (-got +want):\n%s", diff)
	}
}

func TestGitCloneClassifiesLowSpeedTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test git shim is POSIX-only")
	}

	sh := shell.NewTestShell(t)
	binDir := t.TempDir()
	script := "#!/bin/sh\nprintf '%s\\n' 'fatal: unable to access mirror: Operation too slow' >&2\nexit 128\n"
	if err := os.WriteFile(filepath.Join(binDir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	sh.Env.Set("PATH", binDir)

	err := gitClone(t.Context(), sh, nil, nil, "https://mirror.example/repo.git", "repo")
	var gitErr *gitError
	if !errors.As(err, &gitErr) {
		t.Fatalf("gitClone() error = %v, want *gitError", err)
	}
	if gitErr.Type != gitErrorCloneTimeout {
		t.Errorf("gitClone() error type = %d, want gitErrorCloneTimeout", gitErr.Type)
	}
}

func TestHasPartialFilterFlags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		flags []string
		want  bool
	}{
		{name: "no filter", flags: []string{"-v", "--reference", "mirror"}, want: false},
		{name: "filter blob:none with equals", flags: []string{"-v", "--filter=blob:none"}, want: true},
		{name: "filter blob:none separate arg", flags: []string{"-v", "--filter", "blob:none"}, want: true},
		{name: "filter tree:0 with equals", flags: []string{"-v", "--filter=tree:0"}, want: true},
		{name: "filter tree:0 separate arg", flags: []string{"-v", "--filter", "tree:0"}, want: true},
		{name: "multiple filters with blob:none", flags: []string{"--filter=blob:none", "--filter=tree:0"}, want: true},
		{name: "multiple filters separate args", flags: []string{"--filter", "blob:none", "--filter", "tree:0"}, want: true},
		{name: "filter prefix lookalike", flags: []string{"-v", "--filtered"}, want: false},
		{name: "filter without value", flags: []string{"--filter"}, want: false},
		{name: "empty", flags: nil, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := hasPartialFilterFlags(tt.flags); got != tt.want {
				t.Errorf("hasPartialFilterFlags(%v) = %t, want %t", tt.flags, got, tt.want)
			}
		})
	}
}

func TestGitClean(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	var gotLog [][]string
	sh := shell.NewTestShell(t, shell.WithDryRun(true), shell.WithCommandLog(&gotLog))

	absoluteGit, err := sh.AbsolutePath("git")
	if err != nil {
		t.Fatalf("sh.AbsolutePath(git) = %v", err)
	}

	if err := gitClean(ctx, sh, "--foo --bar"); err != nil {
		t.Fatalf(`gitClean(ctx, sh, "--foo --bar") = %v`, err)
	}

	wantLog := [][]string{{absoluteGit, "clean", "--foo", "--bar"}}
	if diff := cmp.Diff(gotLog, wantLog); diff != "" {
		t.Errorf("executed commands diff (-got +want):\n%s", diff)
	}
}

func TestGitCleanSubmodules(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	var gotLog [][]string
	sh := shell.NewTestShell(t, shell.WithDryRun(true), shell.WithCommandLog(&gotLog))

	absoluteGit, err := sh.AbsolutePath("git")
	if err != nil {
		t.Fatalf("sh.AbsolutePath(git) = %v", err)
	}

	if err := gitCleanSubmodules(ctx, sh, "--foo --bar"); err != nil {
		t.Fatalf(`gitCleanSubmodules(ctx, sh, "--foo --bar") = %v`, err)
	}

	wantLog := [][]string{{absoluteGit, "submodule", "foreach", "--recursive", "git clean --foo --bar"}}
	if diff := cmp.Diff(gotLog, wantLog); diff != "" {
		t.Errorf("executed commands diff (-got +want):\n%s", diff)
	}
}

func TestGitFetch(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	var gotLog [][]string
	sh := shell.NewTestShell(t, shell.WithDryRun(true), shell.WithCommandLog(&gotLog))

	absoluteGit, err := sh.AbsolutePath("git")
	if err != nil {
		t.Fatalf("sh.AbsolutePath(git) = %v", err)
	}

	if err := gitFetch(ctx, gitFetchArgs{
		Shell:         sh,
		GitFlags:      []string{"-c", "credential.helper=/path with spaces/helper"},
		GitFetchFlags: "--foo --bar",
		Repository:    "repo",
		RefSpecs:      []string{"ref1", "ref2"},
	}); err != nil {
		t.Fatalf(`gitFetch(ctx, gitFetchArgs{Shell: sh, GitFetchFlags: "--foo --bar", Remote: "repo", RefSpecs: []string{"ref1", "ref2"}} = %v`, err)
	}

	wantLog := [][]string{{
		absoluteGit,
		"-c", "credential.helper=/path with spaces/helper",
		"fetch", "--foo", "--bar", "--", "repo", "ref1", "ref2",
	}}
	if diff := cmp.Diff(gotLog, wantLog); diff != "" {
		t.Errorf("executed commands diff (-got +want):\n%s", diff)
	}
}

func TestGitFetchClassifiesRemoteMissingObjectWithoutRetrying(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test git shim is POSIX-only")
	}

	tests := []struct {
		name   string
		stderr string
		exit   int
	}{
		{
			name:   "protocol v2 not our ref",
			stderr: "fatal: remote error: upload-pack: not our ref " + strings.Repeat("a", 40),
			exit:   128,
		},
		{
			name:   "protocol v0 unadvertised object",
			stderr: "error: Server does not allow request for unadvertised object " + strings.Repeat("a", 40),
			exit:   1,
		},
		{
			name:   "protocol v0 unadvertised object over HTTP",
			stderr: "error: Server does not allow request for unadvertised object " + strings.Repeat("a", 40),
			exit:   128,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sh := shell.NewTestShell(t)
			binDir := t.TempDir()
			attempts := filepath.Join(t.TempDir(), "attempts")
			script := "#!/bin/sh\nprintf x >> \"$ATTEMPTS\"\nprintf '%s\\n' \"$FETCH_STDERR\" >&2\nexit \"$FETCH_EXIT\"\n"
			if err := os.WriteFile(filepath.Join(binDir, "git"), []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			sh.Env.Set("PATH", binDir)
			sh.Env.Set("ATTEMPTS", attempts)
			sh.Env.Set("FETCH_STDERR", tc.stderr)
			sh.Env.Set("FETCH_EXIT", fmt.Sprint(tc.exit))

			err := gitFetch(t.Context(), gitFetchArgs{
				Shell:      sh,
				Repository: "https://mirror.example/repo.git",
				Retry:      true,
				RefSpecs:   []string{strings.Repeat("a", 40)},
			})
			var gitErr *gitError
			if !errors.As(err, &gitErr) {
				t.Fatalf("gitFetch() error = %v, want *gitError", err)
			}
			if gitErr.Type != gitErrorFetchRefNotOnRemote {
				t.Errorf("gitFetch() error type = %d, want gitErrorFetchRefNotOnRemote", gitErr.Type)
			}
			gotAttempts, readErr := os.ReadFile(attempts)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if got, want := len(gotAttempts), 1; got != want {
				t.Errorf("git fetch attempts = %d, want %d", got, want)
			}

			if err := os.WriteFile(attempts, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			err = gitFetchWithFallback(t.Context(), sh, "", strings.Repeat("a", 40))
			if err == nil {
				t.Fatal("gitFetchWithFallback() error = nil, want remote-missing error")
			}
			gotAttempts, readErr = os.ReadFile(attempts)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if got, want := len(gotAttempts), 1; got != want {
				t.Errorf("gitFetchWithFallback() git attempts = %d, want %d (no broad fallback)", got, want)
			}
		})
	}
}

func TestHasGitCommitDisablesLazyFetch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test git shim is POSIX-only")
	}

	commit := strings.Repeat("a", 40)
	sh := shell.NewTestShell(t)
	binDir := t.TempDir()
	captured := filepath.Join(t.TempDir(), "git-no-lazy-fetch")
	script := "#!/bin/sh\nprintf '%s' \"$GIT_NO_LAZY_FETCH\" > \"$CAPTURED_ENV\"\nprintf '%s\\n' \"$EXPECTED_COMMIT\"\n"
	if err := os.WriteFile(filepath.Join(binDir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	sh.Env.Set("PATH", binDir)
	sh.Env.Set("CAPTURED_ENV", captured)
	sh.Env.Set("EXPECTED_COMMIT", commit)

	if !hasGitCommit(t.Context(), sh, ".git", commit) {
		t.Fatal("hasGitCommit() = false, want true")
	}
	got, err := os.ReadFile(captured)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "1" {
		t.Errorf("GIT_NO_LAZY_FETCH = %q, want 1", got)
	}
}

func TestGitLFSFetchCheckout(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	var gotLog [][]string
	sh := shell.NewTestShell(t, shell.WithDryRun(true), shell.WithCommandLog(&gotLog))

	absoluteGit, err := sh.AbsolutePath("git")
	if err != nil {
		t.Fatalf("sh.AbsolutePath(git) = %v", err)
	}

	if err := gitLFSFetchCheckout(ctx, gitLFSFetchCheckoutArgs{
		Shell: sh,
		Retry: true,
	}); err != nil {
		t.Fatalf("gitLFSFetchCheckout(ctx, ...) = %v", err)
	}

	wantLog := [][]string{
		{absoluteGit, "lfs", "fetch"},
		{absoluteGit, "lfs", "checkout"},
	}
	if diff := cmp.Diff(gotLog, wantLog); diff != "" {
		t.Errorf("executed commands diff (-got +want):\n%s", diff)
	}
}

func TestGitLFSFetchCheckoutWithInclude(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	var gotLog [][]string
	sh := shell.NewTestShell(t, shell.WithDryRun(true), shell.WithCommandLog(&gotLog))

	absoluteGit, err := sh.AbsolutePath("git")
	if err != nil {
		t.Fatalf("sh.AbsolutePath(git) = %v", err)
	}

	if err := gitLFSFetchCheckout(ctx, gitLFSFetchCheckoutArgs{
		Shell:        sh,
		Retry:        true,
		FetchInclude: []string{"src/", "docs/"},
	}); err != nil {
		t.Fatalf("gitLFSFetchCheckout(ctx, ...) = %v", err)
	}

	wantLog := [][]string{
		{absoluteGit, "lfs", "fetch", "--include=src/,docs/"},
		{absoluteGit, "lfs", "checkout", "src/", "docs/"},
	}
	if diff := cmp.Diff(gotLog, wantLog); diff != "" {
		t.Errorf("executed commands diff (-got +want):\n%s", diff)
	}
}

func TestGitLFSFetchCheckoutWithCheckoutPathsOverride(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	var gotLog [][]string
	sh := shell.NewTestShell(t, shell.WithDryRun(true), shell.WithCommandLog(&gotLog))

	absoluteGit, err := sh.AbsolutePath("git")
	if err != nil {
		t.Fatalf("sh.AbsolutePath(git) = %v", err)
	}

	checkoutPaths := []string{"src/a.bin", "lib/b.bin"}
	if err := gitLFSFetchCheckout(ctx, gitLFSFetchCheckoutArgs{
		Shell:         sh,
		Retry:         true,
		CheckoutPaths: &checkoutPaths,
	}); err != nil {
		t.Fatalf("gitLFSFetchCheckout(ctx, ...) = %v", err)
	}

	wantLog := [][]string{
		{absoluteGit, "lfs", "fetch"},
		{absoluteGit, "lfs", "checkout", "src/a.bin", "lib/b.bin"},
	}
	if diff := cmp.Diff(gotLog, wantLog); diff != "" {
		t.Errorf("executed commands diff (-got +want):\n%s", diff)
	}
}

func TestGitLFSFetchCheckoutWithEmptyCheckoutPathsSkipsCheckout(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	var gotLog [][]string
	sh := shell.NewTestShell(t, shell.WithDryRun(true), shell.WithCommandLog(&gotLog))

	absoluteGit, err := sh.AbsolutePath("git")
	if err != nil {
		t.Fatalf("sh.AbsolutePath(git) = %v", err)
	}

	checkoutPaths := []string{}
	if err := gitLFSFetchCheckout(ctx, gitLFSFetchCheckoutArgs{
		Shell:         sh,
		Retry:         true,
		CheckoutPaths: &checkoutPaths,
	}); err != nil {
		t.Fatalf("gitLFSFetchCheckout(ctx, ...) = %v", err)
	}

	wantLog := [][]string{
		{absoluteGit, "lfs", "fetch"},
	}
	if diff := cmp.Diff(gotLog, wantLog); diff != "" {
		t.Errorf("executed commands diff (-got +want):\n%s", diff)
	}
}
