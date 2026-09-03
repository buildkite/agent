package job

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/buildkite/agent/v4/internal/job/githttptest"
	"github.com/buildkite/agent/v4/internal/shell"
)

// This file specifies the observable behaviour of the default checkout: given
// a repository and a job configuration, what state does the checkout directory
// end up in? It deliberately asserts on outcomes (HEAD, working tree, git
// config, BUILDKITE_COMMIT) and never on which git commands ran. The
// integration tests in internal/job/integration pin the exact git argv; the
// tests here are what those commands are *for*.
//
// Every scenario runs the whole CheckoutPhase against a real repository served
// over HTTP by githttptest, using the production default flags from
// clicommand/global.go unless the scenario is about a flag.

// checkoutHarness owns one served repository and one checkout directory.
type checkoutHarness struct {
	t           *testing.T
	server      *githttptest.Server
	repoName    string
	checkoutDir string
}

func newCheckoutHarness(t *testing.T) *checkoutHarness {
	t.Helper()

	// Isolate every git invocation (fixtures and the checkout under test) from
	// the host's git configuration: signing requirements, hooks, URL rewrites
	// and credential helpers would otherwise change the outcome.
	for k, v := range isolatedGitEnv(t) {
		t.Setenv(k, v)
	}

	server := githttptest.NewServer()
	t.Cleanup(server.Close)

	h := &checkoutHarness{
		t:           t,
		server:      server,
		repoName:    "project",
		checkoutDir: filepath.Join(t.TempDir(), "checkout"),
	}
	h.serveRepository(h.repoName)
	return h
}

// serveRepository creates and initialises a repository (one commit on main,
// containing README.md) on the test server.
func (h *checkoutHarness) serveRepository(name string) {
	h.t.Helper()
	if err := h.server.CreateRepository(name); err != nil {
		h.t.Fatalf("CreateRepository(%q) error = %v", name, err)
	}
	if out, err := h.server.InitRepository(name); err != nil {
		h.t.Fatalf("InitRepository(%q) error = %v\n%s", name, err, out)
	}
	// InitRepository pushes main, but a bare `git init` leaves HEAD pointing at
	// master. Real hosting providers keep HEAD valid, and a dangling HEAD makes
	// clones behave differently (nothing checked out, sparse init skipped).
	if err := h.server.SetDefaultBranch(name, "main"); err != nil {
		h.t.Fatalf("SetDefaultBranch(%q) error = %v", name, err)
	}
}

// isolatedGitEnv returns environment variables that make git ignore the
// system and user configuration and use a fixed commit identity.
func isolatedGitEnv(t *testing.T) map[string]string {
	t.Helper()
	emptyConfig := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(emptyConfig, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return map[string]string{
		"GIT_CONFIG_NOSYSTEM": "1",
		"GIT_CONFIG_GLOBAL":   emptyConfig,
		"GIT_AUTHOR_NAME":     "Buildkite Agent",
		"GIT_AUTHOR_EMAIL":    "agent@example.com",
		"GIT_COMMITTER_NAME":  "Buildkite Agent",
		"GIT_COMMITTER_EMAIL": "agent@example.com",
		"GIT_TERMINAL_PROMPT": "0",
	}
}

func (h *checkoutHarness) repoURL() string {
	return h.server.RepoURL(h.repoName)
}

// pushBranch creates branch name with one commit on top of main that adds the
// file <name>.txt, and returns the commit hash. Each branch gets a distinct
// commit (githttptest.PushBranch makes byte-identical commits for every
// branch created within the same second, which collapses them to one SHA).
func (h *checkoutHarness) pushBranch(name string) string {
	h.t.Helper()
	return h.commitFiles("refs/heads/"+name, "refs/heads/main", "Add "+name, map[string]string{
		name + ".txt": "This is " + name + ".\n",
	})
}

// commitFiles writes a commit on top of parent that adds files, points ref at
// it in the served repository, and returns its hash. It uses git plumbing
// against the bare repository so no working copy is needed.
func (h *checkoutHarness) commitFiles(ref, parent, message string, files map[string]string) string {
	h.t.Helper()
	repoDir := filepath.Join(h.server.RepositoriesDir(), h.repoName)
	indexed := gitRunner{
		t:   h.t,
		dir: repoDir,
		env: []string{"GIT_INDEX_FILE=" + filepath.Join(h.t.TempDir(), "index")},
	}

	indexed.run("read-tree", parent)
	for name, contents := range files {
		blob := indexed.runStdin(contents, "hash-object", "-w", "--stdin")
		indexed.run("update-index", "--add", "--cacheinfo", "100644,"+blob+","+name)
	}
	tree := indexed.run("write-tree")
	commit := h.git("commit-tree", tree, "-p", parent, "-m", message)
	h.git("update-ref", ref, commit)
	return commit
}

// createRef points refName at commit in the served repository.
func (h *checkoutHarness) createRef(refName, commit string) {
	h.t.Helper()
	if out, err := h.server.CreateRef(h.repoName, refName, commit); err != nil {
		h.t.Fatalf("CreateRef(%q, %q) error = %v\n%s", refName, commit, err, out)
	}
}

// git runs git against the served repository (not the checkout) and returns
// trimmed stdout. Use it to build fixtures the githttptest helpers don't cover.
func (h *checkoutHarness) git(args ...string) string {
	h.t.Helper()
	return gitOutput(h.t, filepath.Join(h.server.RepositoriesDir(), h.repoName), args...)
}

// scratchClone clones the served repository into a temp dir for building
// fixtures, and returns the path.
func (h *checkoutHarness) scratchClone() string {
	h.t.Helper()
	dir := filepath.Join(h.t.TempDir(), "scratch")
	gitOutput(h.t, "", "clone", "--quiet", h.repoURL(), dir)
	return dir
}

// newExecutor returns an Executor configured like a real job with the default
// checkout flags. cfg overrides individual fields; Repository defaults to the
// served repository and PullRequest defaults to "false" (as the backend sends).
func (h *checkoutHarness) newExecutor(cfg ExecutorConfig) *Executor {
	h.t.Helper()

	if cfg.Repository == "" {
		cfg.Repository = h.repoURL()
	}
	if cfg.PullRequest == "" {
		cfg.PullRequest = "false"
	}
	if cfg.GitCleanFlags == "" {
		cfg.GitCleanFlags = "-ffxdq"
	}
	if cfg.GitCloneFlags == "" {
		cfg.GitCloneFlags = "-v"
	}
	if cfg.GitFetchFlags == "" {
		cfg.GitFetchFlags = "-v --prune"
	}
	if cfg.GitCheckoutFlags == "" {
		cfg.GitCheckoutFlags = "-f"
	}
	if cfg.GitMirrorCheckoutMode == "" {
		cfg.GitMirrorCheckoutMode = "reference"
	}
	if cfg.GitMirrorsLockTimeout == 0 {
		cfg.GitMirrorsLockTimeout = 30
	}
	if cfg.GitCommitVerification == "" {
		cfg.GitCommitVerification = GitCommitVerificationOff
	}
	if cfg.CheckoutAttempts == 0 {
		// Keep failure scenarios fast: the default of 6 attempts with
		// exponential backoff takes over a minute to exhaust.
		cfg.CheckoutAttempts = 2
	}

	e := New(cfg)
	e.shell = shell.NewTestShell(h.t, shell.WithSignalGracePeriod(10*time.Millisecond))
	for k, v := range isolatedGitEnv(h.t) {
		e.shell.Env.Set(k, v)
	}
	e.shell.Env.Set("BUILDKITE_BUILD_CHECKOUT_PATH", h.checkoutDir)
	e.shell.Env.Set("BUILDKITE_COMMIT", cfg.Commit)
	// Resolving BUILDKITE_COMMIT is gated on having an access token. Reporting
	// commit metadata to Buildkite calls back into the agent binary and is
	// outside the checkout contract under test here, so mark it done.
	e.shell.Env.Set("BUILDKITE_AGENT_ACCESS_TOKEN", "not-a-real-token")
	e.shell.Env.Set("BUILDKITE_COMMIT_RESOLVED", "true")

	h.t.Cleanup(func() {
		if e.checkoutRoot != nil {
			_ = e.checkoutRoot.Close()
			e.checkoutRoot = nil
		}
	})
	return e
}

// checkout runs the whole checkout phase and fails the test on error.
func (h *checkoutHarness) checkout(e *Executor) {
	h.t.Helper()
	if err := e.CheckoutPhase(h.t.Context()); err != nil {
		h.t.Fatalf("CheckoutPhase() error = %v, want nil", err)
	}
}

// inCheckout runs git in the checkout directory and returns trimmed stdout.
func (h *checkoutHarness) inCheckout(args ...string) string {
	h.t.Helper()
	return gitOutput(h.t, h.checkoutDir, args...)
}

func (h *checkoutHarness) head() string {
	h.t.Helper()
	return h.inCheckout("rev-parse", "HEAD")
}

func (h *checkoutHarness) assertHead(want string) {
	h.t.Helper()
	if got := h.head(); got != want {
		h.t.Errorf("HEAD = %s, want %s", got, want)
	}
}

func (h *checkoutHarness) assertFileExists(name string, want bool) {
	h.t.Helper()
	_, err := os.Stat(filepath.Join(h.checkoutDir, filepath.FromSlash(name)))
	switch {
	case want && err != nil:
		h.t.Errorf("%s: want present, got %v", name, err)
	case !want && !errors.Is(err, os.ErrNotExist):
		h.t.Errorf("%s: want absent, got err = %v", name, err)
	}
}

// assertCleanWorkingTree fails if the checkout has modified, staged, or
// untracked (including ignored) files.
func (h *checkoutHarness) assertCleanWorkingTree() {
	h.t.Helper()
	if status := h.inCheckout("status", "--porcelain", "--ignored"); status != "" {
		h.t.Errorf("working tree is not clean:\n%s", status)
	}
}

func (h *checkoutHarness) writeFile(name, contents string) {
	h.t.Helper()
	path := filepath.Join(h.checkoutDir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		h.t.Fatal(err)
	}
}

// gitRunner runs git for test fixtures and assertions, failing the test on a
// non-zero exit.
type gitRunner struct {
	t   *testing.T
	dir string
	env []string // appended to the process environment
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	return gitRunner{t: t, dir: dir}.run(args...)
}

func (g gitRunner) run(args ...string) string {
	g.t.Helper()
	return g.runStdin("", args...)
}

func (g gitRunner) runStdin(stdin string, args ...string) string {
	g.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = g.dir
	cmd.Env = append(os.Environ(), g.env...)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			g.t.Fatalf("git %v (in %q) error = %v\n%s", args, g.dir, err, exitErr.Stderr)
		}
		g.t.Fatalf("git %v (in %q) error = %v", args, g.dir, err)
	}
	return strings.TrimSpace(string(out))
}

// --- Fresh checkouts -------------------------------------------------------

func TestCheckoutBehaviour_KnownCommitOnBranch(t *testing.T) {
	h := newCheckoutHarness(t)
	commit := h.pushBranch("feature")
	e := h.newExecutor(ExecutorConfig{Commit: commit, Branch: "feature"})

	h.checkout(e)

	h.assertHead(commit)
	h.assertFileExists("feature.txt", true)
	h.assertCleanWorkingTree()
	if got := h.inCheckout("remote", "get-url", "origin"); got != h.repoURL() {
		t.Errorf("origin = %q, want %q", got, h.repoURL())
	}
	if got, _ := e.shell.Env.Get("BUILDKITE_COMMIT"); got != commit {
		t.Errorf("BUILDKITE_COMMIT = %q, want %q (must not change when already a SHA)", got, commit)
	}
	if got := e.shell.Getwd(); got != h.checkoutDir {
		t.Errorf("shell working directory = %q, want checkout dir %q", got, h.checkoutDir)
	}
}

func TestCheckoutBehaviour_HEADCommitResolvesToBranchTip(t *testing.T) {
	h := newCheckoutHarness(t)
	tip := h.pushBranch("feature")
	e := h.newExecutor(ExecutorConfig{Commit: "HEAD", Branch: "feature"})

	h.checkout(e)

	h.assertHead(tip)
	if got, _ := e.shell.Env.Get("BUILDKITE_COMMIT"); got != tip {
		t.Errorf("BUILDKITE_COMMIT = %q, want resolved SHA %q", got, tip)
	}
}

func TestCheckoutBehaviour_HEADCommitIgnoresOtherBranches(t *testing.T) {
	h := newCheckoutHarness(t)
	h.pushBranch("aaa-alphabetically-first")
	tip := h.pushBranch("feature")
	e := h.newExecutor(ExecutorConfig{Commit: "HEAD", Branch: "feature"})

	h.checkout(e)

	h.assertHead(tip)
}

func TestCheckoutBehaviour_CustomRefspec(t *testing.T) {
	h := newCheckoutHarness(t)
	commit := h.pushBranch("feature")
	h.createRef("refs/custom/thing", commit)
	// The build's branch points elsewhere; the refspec must win.
	e := h.newExecutor(ExecutorConfig{Commit: "HEAD", Branch: "main", RefSpec: "refs/custom/thing"})

	h.checkout(e)

	h.assertHead(commit)
}

func TestCheckoutBehaviour_EmptyRepositoryUsesTempDir(t *testing.T) {
	h := newCheckoutHarness(t)
	e := h.newExecutor(ExecutorConfig{Commit: "HEAD", Branch: "main"})
	e.Repository = "" // newExecutor fills in the served repo for ""; this job has none.

	h.checkout(e)

	got, _ := e.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH")
	if got == h.checkoutDir {
		t.Fatalf("BUILDKITE_BUILD_CHECKOUT_PATH = %q, want a fresh temp dir", got)
	}
	if info, err := os.Stat(got); err != nil || !info.IsDir() {
		t.Fatalf("checkout path %q is not a directory: %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(got, ".git")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf(".git in temp checkout: err = %v, want not exist (no repository to clone)", err)
	}
	if e.shell.Getwd() != got {
		t.Errorf("shell working directory = %q, want %q", e.shell.Getwd(), got)
	}
}

// --- Re-using an existing checkout ----------------------------------------

func TestCheckoutBehaviour_ReusesExistingCheckout(t *testing.T) {
	h := newCheckoutHarness(t)
	first := h.pushBranch("first")
	h.checkout(h.newExecutor(ExecutorConfig{Commit: first, Branch: "first"}))

	// Leave evidence that would only survive a fetch, not a re-clone, and dirty
	// the working tree the way a previous build would.
	marker := filepath.Join(h.checkoutDir, ".git", "buildkite-test-marker")
	if err := os.WriteFile(marker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.writeFile("untracked.txt", "left behind")
	h.writeFile("ignored.log", "left behind")
	h.writeFile(".gitignore", "*.log\n") // .gitignore itself is untracked here
	h.writeFile("README.md", "modified by previous build")

	second := h.pushBranch("second")
	h.checkout(h.newExecutor(ExecutorConfig{Commit: second, Branch: "second"}))

	h.assertHead(second)
	h.assertCleanWorkingTree()
	h.assertFileExists("untracked.txt", false)
	h.assertFileExists("ignored.log", false)
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("existing .git was not reused (marker missing: %v)", err)
	}
}

func TestCheckoutBehaviour_CleanCheckoutDiscardsExistingCheckout(t *testing.T) {
	h := newCheckoutHarness(t)
	first := h.pushBranch("first")
	h.checkout(h.newExecutor(ExecutorConfig{Commit: first, Branch: "first"}))
	marker := filepath.Join(h.checkoutDir, ".git", "buildkite-test-marker")
	if err := os.WriteFile(marker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	h.checkout(h.newExecutor(ExecutorConfig{Commit: first, Branch: "first", CleanCheckout: true}))

	h.assertHead(first)
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("clean checkout kept the previous .git (marker err = %v)", err)
	}
}

func TestCheckoutBehaviour_RepositoryRenameUpdatesOrigin(t *testing.T) {
	h := newCheckoutHarness(t)
	commit := h.pushBranch("feature")
	h.checkout(h.newExecutor(ExecutorConfig{Commit: commit, Branch: "feature"}))

	// Same repository, new URL: simulate a rename by serving it under another name.
	const renamed = "project-renamed"
	if err := h.server.CreateRepository(renamed); err != nil {
		t.Fatal(err)
	}
	renamedURL := h.server.RepoURL(renamed)
	h.git("push", "--quiet", "--mirror", renamedURL)

	h.checkout(h.newExecutor(ExecutorConfig{Repository: renamedURL, Commit: commit, Branch: "feature"}))

	h.assertHead(commit)
	if got := h.inCheckout("remote", "get-url", "origin"); got != renamedURL {
		t.Errorf("origin = %q, want renamed URL %q", got, renamedURL)
	}
}

func TestCheckoutBehaviour_CorruptExistingCheckoutSelfHeals(t *testing.T) {
	h := newCheckoutHarness(t)
	commit := h.pushBranch("feature")
	h.checkout(h.newExecutor(ExecutorConfig{Commit: commit, Branch: "feature"}))

	// Destroy the object store; every git command in the existing checkout now
	// fails. The agent must not wedge on this forever: it should discard the
	// directory and clone again.
	if err := os.RemoveAll(filepath.Join(h.checkoutDir, ".git", "objects")); err != nil {
		t.Fatal(err)
	}

	h.checkout(h.newExecutor(ExecutorConfig{Commit: commit, Branch: "feature", CheckoutAttempts: 3}))

	h.assertHead(commit)
	h.assertCleanWorkingTree()
}

func TestCheckoutBehaviour_SkipFetchWhenCommitExistsLocally(t *testing.T) {
	h := newCheckoutHarness(t)
	commit := h.pushBranch("feature")
	h.checkout(h.newExecutor(ExecutorConfig{Commit: commit, Branch: "feature"}))

	// The remote is now unreachable. A checkout of a commit we already have
	// must still succeed when skipping fetches is enabled...
	h.server.Close()
	h.checkout(h.newExecutor(ExecutorConfig{
		Commit: commit, Branch: "feature", GitSkipFetchExistingCommits: true,
	}))
	h.assertHead(commit)

	// ...and must fail (rather than silently build stale state) when it isn't.
	e := h.newExecutor(ExecutorConfig{Commit: commit, Branch: "feature", CheckoutAttempts: 1})
	if err := e.CheckoutPhase(t.Context()); err == nil {
		t.Fatal("CheckoutPhase() with unreachable remote error = nil, want error")
	}
}

// --- GitHub pull requests -------------------------------------------------

func TestCheckoutBehaviour_PullRequestHeadWithKnownCommit(t *testing.T) {
	h := newCheckoutHarness(t)
	commit := h.pushBranch("feature")
	h.createRef("refs/pull/7/head", commit)
	e := h.newExecutor(ExecutorConfig{
		Commit: commit, Branch: "feature", PullRequest: "7", PipelineProvider: "github",
	})

	h.checkout(e)

	h.assertHead(commit)
}

func TestCheckoutBehaviour_PullRequestHeadForcePushedStillBuildsRequestedCommit(t *testing.T) {
	// The build was created for `commit`, but the PR has since been
	// force-pushed so refs/pull/N/head points somewhere else. The job must build
	// the commit it was created for, not whatever the ref points at now.
	h := newCheckoutHarness(t)
	commit := h.pushBranch("feature")
	newer := h.pushBranch("feature-v2")
	h.createRef("refs/pull/7/head", newer)
	e := h.newExecutor(ExecutorConfig{
		Commit: commit, Branch: "feature", PullRequest: "7", PipelineProvider: "github",
	})

	h.checkout(e)

	h.assertHead(commit)
}

func TestCheckoutBehaviour_PullRequestHeadWithHEADCommit(t *testing.T) {
	h := newCheckoutHarness(t)
	h.pushBranch("feature")
	prTip := h.pushBranch("feature-v2")
	h.createRef("refs/pull/7/head", prTip)
	e := h.newExecutor(ExecutorConfig{
		Commit: "HEAD", Branch: "feature", PullRequest: "7", PipelineProvider: "github",
	})

	h.checkout(e)

	h.assertHead(prTip)
	if got, _ := e.shell.Env.Get("BUILDKITE_COMMIT"); got != prTip {
		t.Errorf("BUILDKITE_COMMIT = %q, want %q", got, prTip)
	}
}

func TestCheckoutBehaviour_PullRequestMergeRef(t *testing.T) {
	h := newCheckoutHarness(t)
	head := h.pushBranch("feature")
	base := h.git("rev-parse", "refs/heads/main")
	merge := h.git("commit-tree", head+"^{tree}", "-p", base, "-p", head, "-m", "Merge PR")
	h.createRef("refs/pull/7/merge", merge)

	t.Run("head commit matches", func(t *testing.T) {
		e := h.newExecutor(ExecutorConfig{
			Commit: "HEAD", Branch: "feature", PullRequest: "7", PipelineProvider: "github",
			PullRequestUsingMergeRefspec: true, PullRequestHeadCommit: head,
		})
		h.checkout(e)
		h.assertHead(merge)
	})

	t.Run("head commit mismatch fails", func(t *testing.T) {
		e := h.newExecutor(ExecutorConfig{
			Commit: "HEAD", Branch: "feature", PullRequest: "7", PipelineProvider: "github",
			PullRequestUsingMergeRefspec: true, PullRequestHeadCommit: base,
			CheckoutAttempts: 1,
		})
		if err := e.CheckoutPhase(t.Context()); err == nil {
			t.Fatal("CheckoutPhase() error = nil, want error: merge ref's second parent is not the PR head")
		}
	})
}

// --- Commit verification ---------------------------------------------------

func TestCheckoutBehaviour_StrictVerificationRejectsCommitNotOnBranch(t *testing.T) {
	h := newCheckoutHarness(t)
	onBranch := h.pushBranch("feature")
	elsewhere := h.pushBranch("unrelated")

	t.Run("commit on branch passes", func(t *testing.T) {
		e := h.newExecutor(ExecutorConfig{
			Commit: onBranch, Branch: "feature", GitCommitVerification: GitCommitVerificationStrict,
		})
		h.checkout(e)
		h.assertHead(onBranch)
	})

	t.Run("commit off branch fails fast", func(t *testing.T) {
		e := h.newExecutor(ExecutorConfig{
			Commit: elsewhere, Branch: "feature", GitCommitVerification: GitCommitVerificationStrict,
			CheckoutAttempts: 6, // must not be exhausted: verification failure is final
		})
		start := time.Now()
		err := e.CheckoutPhase(t.Context())
		if !errors.Is(err, ErrCommitVerificationFailed) {
			t.Fatalf("CheckoutPhase() error = %v, want ErrCommitVerificationFailed", err)
		}
		if elapsed := time.Since(start); elapsed > 10*time.Second {
			t.Errorf("verification failure took %s; it must not be retried", elapsed)
		}
	})

	t.Run("off mode allows commit off branch", func(t *testing.T) {
		e := h.newExecutor(ExecutorConfig{
			Commit: elsewhere, Branch: "feature", GitCommitVerification: GitCommitVerificationOff,
		})
		h.checkout(e)
		h.assertHead(elsewhere)
	})
}

// --- Submodules ----------------------------------------------------------------

func TestCheckoutBehaviour_Submodules(t *testing.T) {
	h := newCheckoutHarness(t)

	const subName = "library"
	h.serveRepository(subName)
	scratch := h.scratchClone()
	gitOutput(t, scratch, "submodule", "add", "--quiet", h.server.RepoURL(subName), "vendor/library")
	gitOutput(t, scratch, "commit", "--quiet", "-m", "Add submodule")
	gitOutput(t, scratch, "push", "--quiet", "origin", "HEAD:refs/heads/with-submodule")
	commit := gitOutput(t, scratch, "rev-parse", "HEAD")

	t.Run("enabled", func(t *testing.T) {
		h.checkout(h.newExecutor(ExecutorConfig{Commit: commit, Branch: "with-submodule", GitSubmodules: true}))
		h.assertHead(commit)
		h.assertFileExists("vendor/library/README.md", true)
		h.assertCleanWorkingTree()
	})

	t.Run("disabled leaves submodule empty", func(t *testing.T) {
		if err := os.RemoveAll(h.checkoutDir); err != nil {
			t.Fatal(err)
		}
		h.checkout(h.newExecutor(ExecutorConfig{Commit: commit, Branch: "with-submodule", GitSubmodules: false}))
		h.assertHead(commit)
		h.assertFileExists("vendor/library", true)
		h.assertFileExists("vendor/library/README.md", false)
	})
}

// --- On-host git mirrors ---------------------------------------------------

func TestCheckoutBehaviour_OnHostMirror(t *testing.T) {
	h := newCheckoutHarness(t)
	commit := h.pushBranch("feature")
	mirrorsPath := t.TempDir()
	mirrorDir := filepath.Join(mirrorsPath, dirForRepository(h.repoURL()))

	t.Run("reference mode shares objects with the mirror", func(t *testing.T) {
		e := h.newExecutor(ExecutorConfig{
			Commit: commit, Branch: "feature", GitMirrorsPath: mirrorsPath, GitMirrorCheckoutMode: "reference",
		})
		h.checkout(e)

		h.assertHead(commit)
		if got := gitOutput(t, mirrorDir, "rev-parse", "--is-bare-repository"); got != "true" {
			t.Errorf("mirror at %s is not a bare repository", mirrorDir)
		}
		if got := gitOutput(t, mirrorDir, "rev-parse", commit+"^{commit}"); got != commit {
			t.Errorf("mirror does not contain the build commit")
		}
		alternates, err := os.ReadFile(filepath.Join(h.checkoutDir, ".git", "objects", "info", "alternates"))
		if err != nil {
			t.Fatalf("reference-mode checkout has no alternates file: %v", err)
		}
		if !strings.Contains(string(alternates), filepath.Join(mirrorDir, "objects")) {
			t.Errorf("alternates = %q, want reference to %s", alternates, mirrorDir)
		}
		if got, _ := e.shell.Env.Get("BUILDKITE_REPO_MIRROR"); got != mirrorDir {
			t.Errorf("BUILDKITE_REPO_MIRROR = %q, want %q", got, mirrorDir)
		}
	})

	t.Run("dissociate mode copies objects", func(t *testing.T) {
		e := h.newExecutor(ExecutorConfig{
			Commit: commit, Branch: "feature", GitMirrorsPath: mirrorsPath, GitMirrorCheckoutMode: "dissociate",
		})
		h.checkout(e)

		h.assertHead(commit)
		h.assertFileExists(".git/objects/info/alternates", false)
	})

	t.Run("mirror is reused and updated for new commits", func(t *testing.T) {
		newer := h.pushBranch("feature-v2")
		e := h.newExecutor(ExecutorConfig{
			Commit: newer, Branch: "feature-v2", GitMirrorsPath: mirrorsPath, GitMirrorCheckoutMode: "dissociate",
		})
		h.checkout(e)

		h.assertHead(newer)
		if got := gitOutput(t, mirrorDir, "rev-parse", newer+"^{commit}"); got != newer {
			t.Errorf("mirror was not updated with the new commit")
		}
	})

	t.Run("mirror is updated for a HEAD build", func(t *testing.T) {
		// Without a known commit there is nothing to look for in the existing
		// mirror, so it must be brought up to date with the branch tip.
		tip := h.pushBranch("feature-v3")
		e := h.newExecutor(ExecutorConfig{
			Commit: "HEAD", Branch: "feature-v3", GitMirrorsPath: mirrorsPath, GitMirrorCheckoutMode: "dissociate",
		})
		h.checkout(e)

		h.assertHead(tip)
		if got := gitOutput(t, mirrorDir, "rev-parse", "refs/heads/feature-v3"); got != tip {
			t.Errorf("mirror refs/heads/feature-v3 = %s, want branch tip %s", got, tip)
		}
	})
}

// --- Sparse checkout ---------------------------------------------------------

func TestCheckoutBehaviour_SparseCheckout(t *testing.T) {
	h := newCheckoutHarness(t)
	version, err := gitVersion(t.Context(), shell.NewTestShell(t))
	if err != nil || !version.atLeast(minGitSparseCheckout) {
		t.Skipf("sparse checkout needs git >= %s (have %v, err %v)", minGitSparseCheckout, version, err)
	}
	commit := h.commitFiles("refs/heads/dirs", "refs/heads/main", "Add dirs", map[string]string{
		"src/main.txt":   "src\n",
		"docs/readme.md": "docs\n",
	})

	h.checkout(h.newExecutor(ExecutorConfig{
		Commit: commit, Branch: "dirs", GitSparseCheckoutPaths: []string{"src/"},
	}))

	h.assertHead(commit)
	h.assertFileExists("src/main.txt", true)
	h.assertFileExists("README.md", true) // top-level files are part of the cone
	h.assertFileExists("docs/readme.md", false)

	// Re-using the checkout without sparse paths restores the full tree.
	h.checkout(h.newExecutor(ExecutorConfig{Commit: commit, Branch: "dirs"}))
	h.assertFileExists("docs/readme.md", true)
}
