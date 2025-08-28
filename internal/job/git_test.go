package job

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/buildkite/agent/v3/internal/shell"
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
	ctx := context.Background()
	sh := shell.NewTestShell(t, shell.WithDryRun(true))
	err := gitCheckout(ctx, sh, "", "--nope")
	if got, want := err.Error(), `"--nope" is not a valid git ref format`; got != want {
		t.Errorf(`gitCheckout(ctx, sh, "", "--nope") error = %q, want %q`, got, want)
	}
}

func TestGitCheckout(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	var gotLog [][]string
	sh := shell.NewTestShell(t, shell.WithDryRun(true), shell.WithCommandLog(&gotLog))

	absoluteGit, err := sh.AbsolutePath("git")
	if err != nil {
		t.Fatalf("sh.AbsolutePath(git) = %v", err)
	}

	if err := gitCheckout(ctx, sh, "-f -q", "main"); err != nil {
		t.Fatalf(`gitCheckout(ctx, sh, "-f -q", "main") = %v`, err)
	}

	wantLog := [][]string{{absoluteGit, "checkout", "-f", "-q", "main"}}
	if diff := cmp.Diff(gotLog, wantLog); diff != "" {
		t.Errorf("executed commands diff (-got +want):\n%s", diff)
	}
}

func TestGitCheckoutSketchyArgs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	sh := shell.NewTestShell(t, shell.WithDryRun(true))
	err := gitCheckout(ctx, sh, "-f -q", "  --hello")
	if got, want := err.Error(), `"  --hello" is not a valid git ref format`; got != want {
		t.Errorf(`gitCheckout(ctx, sh, "-f -q", "  --hello") error = %q, want %q`, got, want)
	}
}

func TestGitClone(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	var gotLog [][]string
	sh := shell.NewTestShell(t, shell.WithDryRun(true), shell.WithCommandLog(&gotLog))

	absoluteGit, err := sh.AbsolutePath("git")
	if err != nil {
		t.Fatalf("sh.AbsolutePath(git) = %v", err)
	}

	if err := gitClone(ctx, sh, "-v --references url", "repo", "dir"); err != nil {
		t.Fatalf(`gitClone(ctx, sh, "-v --references url", "repo", "dir") = %v`, err)
	}

	wantLog := [][]string{{absoluteGit, "clone", "-v", "--references", "url", "--", "repo", "dir"}}
	if diff := cmp.Diff(gotLog, wantLog); diff != "" {
		t.Errorf("executed commands diff (-got +want):\n%s", diff)
	}
}

func TestGitClean(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

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
	ctx := context.Background()

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
	ctx := context.Background()

	var gotLog [][]string
	sh := shell.NewTestShell(t, shell.WithDryRun(true), shell.WithCommandLog(&gotLog))

	absoluteGit, err := sh.AbsolutePath("git")
	if err != nil {
		t.Fatalf("sh.AbsolutePath(git) = %v", err)
	}

	if err := gitFetch(ctx, gitFetchArgs{
		Shell:         sh,
		GitFetchFlags: "--foo --bar",
		Repository:    "repo",
		RefSpecs:      []string{"ref1", "ref2"},
	}); err != nil {
		t.Fatalf(`gitFetch(ctx, gitFetchArgs{Shell: sh, GitFetchFlags: "--foo --bar", Remote: "repo", RefSpecs: []string{"ref1", "ref2"}} = %v`, err)
	}

	wantLog := [][]string{{absoluteGit, "fetch", "--foo", "--bar", "--", "repo", "ref1", "ref2"}}
	if diff := cmp.Diff(gotLog, wantLog); diff != "" {
		t.Errorf("executed commands diff (-got +want):\n%s", diff)
	}
}

func TestGitSparseCheckoutInit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Test cone mode
	{
		var gotLog [][]string
		sh := shell.NewTestShell(t, shell.WithDryRun(true), shell.WithCommandLog(&gotLog))

		absoluteGit, err := sh.AbsolutePath("git")
		if err != nil {
			t.Fatalf("sh.AbsolutePath(git) = %v", err)
		}

		err = gitSparseCheckoutInit(ctx, sh, true)
		if err != nil {
			t.Fatalf("gitSparseCheckoutInit(ctx, sh, true) = %v", err)
		}

		wantLog := [][]string{{absoluteGit, "sparse-checkout", "init", "--cone"}}
		if diff := cmp.Diff(gotLog, wantLog); diff != "" {
			t.Errorf("executed commands diff (-got +want):\n%s", diff)
		}
	}

	// Test non-cone mode
	{
		var gotLog [][]string
		sh := shell.NewTestShell(t, shell.WithDryRun(true), shell.WithCommandLog(&gotLog))

		absoluteGit, err := sh.AbsolutePath("git")
		if err != nil {
			t.Fatalf("sh.AbsolutePath(git) = %v", err)
		}

		err = gitSparseCheckoutInit(ctx, sh, false)
		if err != nil {
			t.Fatalf("gitSparseCheckoutInit(ctx, sh, false) = %v", err)
		}

		wantLog := [][]string{{absoluteGit, "sparse-checkout", "init"}}
		if diff := cmp.Diff(gotLog, wantLog); diff != "" {
			t.Errorf("executed commands diff (-got +want):\n%s", diff)
		}
	}
}

func TestGitSparseCheckoutSet(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Test with single path
	{
		var gotLog [][]string
		sh := shell.NewTestShell(t, shell.WithDryRun(true), shell.WithCommandLog(&gotLog))

		absoluteGit, err := sh.AbsolutePath("git")
		if err != nil {
			t.Fatalf("sh.AbsolutePath(git) = %v", err)
		}

		paths := []string{"src/frontend"}
		err = gitSparseCheckoutSet(ctx, sh, paths)
		if err != nil {
			t.Fatalf("gitSparseCheckoutSet(ctx, sh, %v) = %v", paths, err)
		}

		wantLog := [][]string{{absoluteGit, "sparse-checkout", "set", "src/frontend"}}
		if diff := cmp.Diff(gotLog, wantLog); diff != "" {
			t.Errorf("executed commands diff (-got +want):\n%s", diff)
		}
	}

	// Test with multiple paths
	{
		var gotLog [][]string
		sh := shell.NewTestShell(t, shell.WithDryRun(true), shell.WithCommandLog(&gotLog))

		absoluteGit, err := sh.AbsolutePath("git")
		if err != nil {
			t.Fatalf("sh.AbsolutePath(git) = %v", err)
		}

		paths := []string{"src/frontend", "src/backend", "docs"}
		err = gitSparseCheckoutSet(ctx, sh, paths)
		if err != nil {
			t.Fatalf("gitSparseCheckoutSet(ctx, sh, %v) = %v", paths, err)
		}

		wantLog := [][]string{{absoluteGit, "sparse-checkout", "set", "src/frontend", "src/backend", "docs"}}
		if diff := cmp.Diff(gotLog, wantLog); diff != "" {
			t.Errorf("executed commands diff (-got +want):\n%s", diff)
		}
	}

	// Test with empty paths
	{
		sh := shell.NewTestShell(t, shell.WithDryRun(true))
		err := gitSparseCheckoutSet(ctx, sh, []string{})
		if err == nil {
			t.Errorf("gitSparseCheckoutSet(ctx, sh, []string{}) = nil, want error")
		}
		if got, want := err.Error(), "no paths provided for sparse checkout"; got != want {
			t.Errorf("gitSparseCheckoutSet(ctx, sh, []string{}) error = %q, want %q", got, want)
		}
	}
}

func TestGitSparseCheckoutDisable(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	var gotLog [][]string
	sh := shell.NewTestShell(t, shell.WithDryRun(true), shell.WithCommandLog(&gotLog))

	absoluteGit, err := sh.AbsolutePath("git")
	if err != nil {
		t.Fatalf("sh.AbsolutePath(git) = %v", err)
	}

	err = gitSparseCheckoutDisable(ctx, sh)
	if err != nil {
		t.Fatalf("gitSparseCheckoutDisable(ctx, sh) = %v", err)
	}

	wantLog := [][]string{{absoluteGit, "sparse-checkout", "disable"}}
	if diff := cmp.Diff(gotLog, wantLog); diff != "" {
		t.Errorf("executed commands diff (-got +want):\n%s", diff)
	}
}

func TestIsSparseCheckoutEnabled(t *testing.T) {
	t.Parallel()

	// Create a temporary directory for testing
	tmpDir := t.TempDir()
	gitDir := filepath.Join(tmpDir, ".git")
	infoDir := filepath.Join(gitDir, "info")
	sparseCheckoutFile := filepath.Join(infoDir, "sparse-checkout")

	// Create shell with working directory set to tmpDir
	sh := shell.NewTestShell(t)
	sh.Chdir(tmpDir)

	// Test 1: No .git directory
	if isSparseCheckoutEnabled(sh) {
		t.Error("expected sparse checkout to be disabled when .git doesn't exist")
	}

	// Test 2: .git exists but no sparse-checkout file
	os.MkdirAll(infoDir, 0755)
	if isSparseCheckoutEnabled(sh) {
		t.Error("expected sparse checkout to be disabled when sparse-checkout file doesn't exist")
	}

	// Test 3: sparse-checkout file exists but is empty
	os.WriteFile(sparseCheckoutFile, []byte{}, 0644)
	if isSparseCheckoutEnabled(sh) {
		t.Error("expected sparse checkout to be disabled when sparse-checkout file is empty")
	}

	// Test 4: sparse-checkout file exists with content
	os.WriteFile(sparseCheckoutFile, []byte("dir1\ndir2\n"), 0644)
	if !isSparseCheckoutEnabled(sh) {
		t.Error("expected sparse checkout to be enabled when sparse-checkout file has content")
	}
}
