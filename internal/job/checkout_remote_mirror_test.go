package job

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/internal/job/githttptest"
	"github.com/buildkite/agent/v3/internal/shell"
)

func TestIsFullCommitSHA(t *testing.T) {
	t.Parallel()

	valid := strings.Repeat("0123456789abcdef", 2) + "01234567"
	tests := map[string]bool{
		valid:                   true,
		strings.ToUpper(valid):  false,
		valid[:39]:              false,
		valid + "0":             false,
		"HEAD":                  false,
		"main":                  false,
		strings.Repeat("g", 40): false,
	}
	for commit, want := range tests {
		if got := isFullCommitSHA(commit); got != want {
			t.Errorf("isFullCommitSHA(%q) = %t, want %t", commit, got, want)
		}
	}
}

func TestShouldAttemptRemoteMirror(t *testing.T) {
	t.Parallel()

	commit := strings.Repeat("a", 40)
	base := ExecutorConfig{
		Commit:             commit,
		GitRemoteMirrorURL: "https://mirror.example.com/acme/widgets.git",
		PullRequest:        "false",
	}
	tests := []struct {
		name             string
		mutate           func(*ExecutorConfig)
		previousAttempts int
		want             bool
	}{
		{name: "eligible", want: true},
		{name: "empty URL", mutate: func(c *ExecutorConfig) { c.GitRemoteMirrorURL = "" }},
		{name: "HEAD", mutate: func(c *ExecutorConfig) { c.Commit = "HEAD" }},
		{name: "short SHA", mutate: func(c *ExecutorConfig) { c.Commit = commit[:12] }},
		{name: "uppercase SHA", mutate: func(c *ExecutorConfig) { c.Commit = strings.ToUpper(commit) }},
		{name: "tag", mutate: func(c *ExecutorConfig) { c.Tag = "v1.0.0" }},
		{name: "pull request", mutate: func(c *ExecutorConfig) { c.PullRequest = "42" }},
		{name: "custom refspec", mutate: func(c *ExecutorConfig) { c.RefSpec = "refs/heads/main" }},
		{name: "later checkout attempt", previousAttempts: 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			if tc.mutate != nil {
				tc.mutate(&cfg)
			}
			e := &Executor{ExecutorConfig: cfg}
			if got := e.shouldAttemptRemoteMirror(tc.previousAttempts); got != tc.want {
				t.Errorf("shouldAttemptRemoteMirror(%d) = %t, want %t", tc.previousAttempts, got, tc.want)
			}
		})
	}
}

func TestRemoteMirrorHitChecksOutExactCommitWithCanonicalOrigin(t *testing.T) {
	e := newRemoteMirrorTestExecutor(t)
	canonical, commit := setupCheckoutTestRepo(t, e, "canonical")
	mirror := copyRemoteMirrorRepository(t, canonical.RepoURL("canonical"), "mirror")

	e.Commit = commit
	e.Branch = "feature-branch"
	e.PullRequest = "false"
	e.GitRemoteMirrorURL = mirror.RepoURL("mirror")
	canonicalURL := e.Repository

	// A mirror hit must not need canonical transport.
	canonical.Close()

	if err := e.defaultCheckoutPhase(t.Context(), 0); err != nil {
		t.Fatalf("defaultCheckoutPhase() error = %v", err)
	}

	assertCheckedOutCommit(t, e, commit)
	assertOriginURL(t, e, canonicalURL)
	assertNoMutableRefs(t, e)
}

func TestRemoteMirrorMissFallsBackToCanonical(t *testing.T) {
	e := newRemoteMirrorTestExecutor(t)
	_, commit := setupCheckoutTestRepo(t, e, "canonical")

	mirror := githttptest.NewServer()
	t.Cleanup(mirror.Close)
	if err := mirror.CreateRepository("mirror"); err != nil {
		t.Fatal(err)
	}
	if _, err := mirror.InitRepository("mirror"); err != nil {
		t.Fatal(err)
	}

	e.Commit = commit
	e.Branch = "feature-branch"
	e.PullRequest = "false"
	e.GitRemoteMirrorURL = mirror.RepoURL("mirror")

	if err := e.defaultCheckoutPhase(t.Context(), 0); err != nil {
		t.Fatalf("defaultCheckoutPhase() error = %v", err)
	}

	assertCheckedOutCommit(t, e, commit)
	assertOriginURL(t, e, e.Repository)
}

func TestRemoteMirrorFailureFallsBackToCanonical(t *testing.T) {
	e := newRemoteMirrorTestExecutor(t)
	_, commit := setupCheckoutTestRepo(t, e, "canonical")

	e.Commit = commit
	e.Branch = "feature-branch"
	e.PullRequest = "false"
	e.GitRemoteMirrorURL = "http://127.0.0.1:1/acme/widgets.git"

	if err := e.defaultCheckoutPhase(t.Context(), 0); err != nil {
		t.Fatalf("defaultCheckoutPhase() error = %v", err)
	}

	assertCheckedOutCommit(t, e, commit)
	assertOriginURL(t, e, e.Repository)
}

func TestIneligibleRemoteMirrorTargetsUseCanonical(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Executor)
	}{
		{name: "HEAD", mutate: func(e *Executor) { e.Commit = "HEAD" }},
		{name: "short SHA", mutate: func(e *Executor) { e.Commit = e.Commit[:12] }},
		{name: "uppercase SHA", mutate: func(e *Executor) { e.Commit = strings.ToUpper(e.Commit) }},
		{name: "tag", mutate: func(e *Executor) { e.Tag = "v1.0.0" }},
		{name: "pull request", mutate: func(e *Executor) { e.PullRequest = "42" }},
		{name: "custom refspec", mutate: func(e *Executor) { e.RefSpec = "refs/heads/feature-branch" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newRemoteMirrorTestExecutor(t)
			_, commit := setupCheckoutTestRepo(t, e, "canonical")
			e.Commit = commit
			e.Branch = "feature-branch"
			e.PullRequest = "false"
			e.GitRemoteMirrorURL = "http://127.0.0.1:1/acme/widgets.git"
			tc.mutate(e)

			if err := e.defaultCheckoutPhase(t.Context(), 0); err != nil {
				t.Fatalf("defaultCheckoutPhase() error = %v", err)
			}
		})
	}
}

func newRemoteMirrorTestExecutor(t *testing.T) *Executor {
	t.Helper()
	sh, err := shell.New()
	if err != nil {
		t.Fatal(err)
	}
	return &Executor{
		shell: sh,
		ExecutorConfig: ExecutorConfig{
			GitCheckoutFlags: "-f",
			GitCleanFlags:    "-ffxdq",
			GitCloneFlags:    "-v",
			GitFetchFlags:    "-v --prune",
		},
	}
}

func copyRemoteMirrorRepository(t *testing.T, sourceURL, name string) *githttptest.Server {
	t.Helper()
	mirror := githttptest.NewServer()
	t.Cleanup(mirror.Close)
	if err := mirror.CreateRepository(name); err != nil {
		t.Fatal(err)
	}

	cloneDir := t.TempDir()
	runGitForRemoteMirrorTest(t, "", "clone", "--mirror", sourceURL, cloneDir)
	runGitForRemoteMirrorTest(t, cloneDir, "push", "--mirror", mirror.RepoURL(name))
	return mirror
}

func runGitForRemoteMirrorTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Buildkite Agent",
		"GIT_AUTHOR_EMAIL=agent@example.com",
		"GIT_COMMITTER_NAME=Buildkite Agent",
		"GIT_COMMITTER_EMAIL=agent@example.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v error = %v\n%s", args, err, out)
	}
}

func assertCheckedOutCommit(t *testing.T, e *Executor, want string) {
	t.Helper()
	got, err := e.shell.Command("git", "rev-parse", "HEAD").RunAndCaptureStdout(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("checked out commit = %q, want %q", got, want)
	}
}

func assertOriginURL(t *testing.T, e *Executor, want string) {
	t.Helper()
	got, err := e.shell.Command("git", "remote", "get-url", "origin").RunAndCaptureStdout(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("origin URL = %q, want %q", got, want)
	}
}

func assertNoMutableRefs(t *testing.T, e *Executor) {
	t.Helper()
	got, err := e.shell.Command(
		"git", "for-each-ref", "--format=%(refname)",
		"refs/heads", "refs/remotes", "refs/tags",
	).RunAndCaptureStdout(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(got) != "" {
		t.Errorf("mirror-derived mutable refs remain: %s", got)
	}

	configPath := filepath.Join(e.shell.Getwd(), ".git", "config")
	config, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(config), e.GitRemoteMirrorURL) {
		t.Errorf(".git/config retains remote mirror URL")
	}
	if strings.Contains(string(config), "credential.helper") {
		t.Errorf(".git/config persists credential helper")
	}
}
