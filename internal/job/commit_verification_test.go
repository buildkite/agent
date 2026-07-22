package job

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/internal/job/githttptest"
	"github.com/buildkite/agent/v3/internal/shell"
)

// newFileBackedRepo creates an empty bare git repository served over file://
// plus a working clone to build history in (the shell is left cd'd into the
// clone). It returns the shell, the file:// URL, and a helper that commits a
// one-line file and returns its SHA. A file:// remote is used rather than
// githttptest because shallow deepening over the test server's stateless-rpc
// HTTP transport is not reliable across git versions. prefix names the temp
// directories so failures point at the right fixture.
func newFileBackedRepo(t *testing.T, ctx context.Context, prefix string) (*shell.Shell, string, func(name string) string) {
	t.Helper()
	t.Setenv("GIT_AUTHOR_NAME", "Buildkite Agent")
	t.Setenv("GIT_AUTHOR_EMAIL", "agent@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Buildkite Agent")
	t.Setenv("GIT_COMMITTER_EMAIL", "agent@example.com")

	bareDir, err := os.MkdirTemp("", prefix+"-bare-")
	if err != nil {
		t.Fatalf("MkdirTemp error = %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(bareDir) }) //nolint:errcheck // Best-effort cleanup.

	sh, err := shell.New()
	if err != nil {
		t.Fatalf("shell.New() error = %v", err)
	}
	if err := sh.Command("git", "init", "--bare", bareDir).Run(ctx); err != nil {
		t.Fatalf("git init --bare error = %v", err)
	}
	urlPath := filepath.ToSlash(bareDir)
	if !strings.HasPrefix(urlPath, "/") {
		urlPath = "/" + urlPath
	}
	repoURL := "file://" + urlPath

	workDir, err := os.MkdirTemp("", prefix+"-work-")
	if err != nil {
		t.Fatalf("MkdirTemp error = %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(workDir) }) //nolint:errcheck // Best-effort cleanup.
	if err := sh.Command("git", "clone", repoURL, workDir).Run(ctx); err != nil {
		t.Fatalf("git clone error = %v", err)
	}
	if err := sh.Chdir(workDir); err != nil {
		t.Fatalf("Chdir error = %v", err)
	}

	commit := func(name string) string {
		if err := os.WriteFile(filepath.Join(workDir, name), []byte(name), 0o600); err != nil {
			t.Fatalf("WriteFile error = %v", err)
		}
		if err := sh.Command("git", "add", name).Run(ctx); err != nil {
			t.Fatalf("git add error = %v", err)
		}
		if err := sh.Command("git", "commit", "-m", "commit "+name).Run(ctx); err != nil {
			t.Fatalf("git commit error = %v", err)
		}
		sha, err := sh.Command("git", "rev-parse", "HEAD").RunAndCaptureStdout(ctx)
		if err != nil {
			t.Fatalf("rev-parse HEAD error = %v", err)
		}
		return strings.TrimSpace(sha)
	}

	return sh, repoURL, commit
}

// setupFileBackedRepo creates a bare git repository served over file:// with a
// featureBranch (commits a <- b <- c) and a divergent "other" branch (a <- o).
// It returns the file:// URL, a deep ancestor of featureBranch (a, beyond a
// depth=1 clone's boundary), and an off-branch commit (o, present on the remote
// but not on featureBranch). A file:// remote is used rather than githttptest
// because shallow deepening over the test server's stateless-rpc HTTP transport
// is not reliable across git versions.
func setupFileBackedRepo(t *testing.T, ctx context.Context, featureBranch string) (repoURL, deepAncestor, offBranchCommit string) {
	t.Helper()
	sh, repoURL, commit := newFileBackedRepo(t, ctx, "verify")

	// featureBranch: a <- b <- c, where a is two commits below the tip.
	deepAncestor = commit("a.txt")
	commit("b.txt")
	commit("c.txt")
	if err := sh.Command("git", "branch", "-m", featureBranch).Run(ctx); err != nil {
		t.Fatalf("git branch -m %q error = %v", featureBranch, err)
	}
	if err := sh.Command("git", "push", "origin", featureBranch).Run(ctx); err != nil {
		t.Fatalf("git push %q error = %v", featureBranch, err)
	}

	// other: a <- o, diverging from featureBranch so o is not an ancestor of it.
	if err := sh.Command("git", "checkout", "-b", "other", deepAncestor).Run(ctx); err != nil {
		t.Fatalf("git checkout -b other error = %v", err)
	}
	offBranchCommit = commit("other.txt")
	if err := sh.Command("git", "push", "origin", "other").Run(ctx); err != nil {
		t.Fatalf("git push other error = %v", err)
	}

	return repoURL, deepAncestor, offBranchCommit
}

// setupTagBranchCollisionRepo creates a bare repo served over file:// where a
// branch and a tag share the name "release". The branch is a <- b; the tag points
// at a divergent commit t (a <- t) that is not reachable from the branch. It
// returns the file:// URL and the off-branch commit the tag points at. This is
// the ambiguous case: git resolves a bare name against refs/tags/ before
// refs/heads/, so an unqualified fetch of "release" pins FETCH_HEAD to the tag.
func setupTagBranchCollisionRepo(t *testing.T, ctx context.Context) (repoURL, offBranchTagCommit string) {
	t.Helper()
	sh, repoURL, commit := newFileBackedRepo(t, ctx, "verify-collision")

	// release branch: a <- b.
	base := commit("a.txt")
	commit("b.txt")
	if err := sh.Command("git", "branch", "-m", "release").Run(ctx); err != nil {
		t.Fatalf("git branch -m release error = %v", err)
	}
	if err := sh.Command("git", "push", "origin", "release").Run(ctx); err != nil {
		t.Fatalf("git push release error = %v", err)
	}

	// tag "release" on a divergent commit t (a <- t), not reachable from the branch.
	if err := sh.Command("git", "checkout", "-b", "tagline", base).Run(ctx); err != nil {
		t.Fatalf("git checkout -b tagline error = %v", err)
	}
	offBranchTagCommit = commit("t.txt")
	if err := sh.Command("git", "tag", "release").Run(ctx); err != nil {
		t.Fatalf("git tag release error = %v", err)
	}
	if err := sh.Command("git", "push", "origin", "refs/tags/release").Run(ctx); err != nil {
		t.Fatalf("git push tag release error = %v", err)
	}

	return repoURL, offBranchTagCommit
}

func TestVerifyCommit(t *testing.T) {
	// Table-driven tests for the skip conditions — these don't need a real git repo
	skipTests := []struct {
		name   string
		config ExecutorConfig
	}{
		{
			name: "skips when verification is disabled",
			config: ExecutorConfig{
				GitCommitVerification: "",
				Commit:                "abc123",
				Branch:                "main",
			},
		},
		{
			name: "skips when commit is HEAD",
			config: ExecutorConfig{
				GitCommitVerification: "strict",
				Commit:                "HEAD",
				Branch:                "main",
			},
		},
		{
			name: "skips when branch is empty",
			config: ExecutorConfig{
				GitCommitVerification: "strict",
				Commit:                "abc123",
				Branch:                "",
			},
		},
		{
			name: "skips for PR builds",
			config: ExecutorConfig{
				GitCommitVerification: "strict",
				Commit:                "abc123",
				Branch:                "main",
				PullRequest:           "123",
			},
		},
		{
			name: "skips for tag builds",
			config: ExecutorConfig{
				GitCommitVerification: "strict",
				Commit:                "abc123",
				Branch:                "main",
				Tag:                   "v1.0.0",
			},
		},
		{
			name: "skips for custom refspec",
			config: ExecutorConfig{
				GitCommitVerification: "strict",
				Commit:                "abc123",
				Branch:                "main",
				RefSpec:               "refs/custom/spec",
			},
		},
	}

	for _, tt := range skipTests {
		t.Run(tt.name, func(t *testing.T) {
			sh, err := shell.New()
			if err != nil {
				t.Fatalf("shell.New() error = %v", err)
			}
			e := &Executor{
				shell:          sh,
				ExecutorConfig: tt.config,
			}
			err = e.verifyCommit(t.Context())
			if err != nil {
				t.Errorf("verifyCommit() error = %v, want nil", err)
			}
		})
	}

	// Git-dependent tests — these need a real repo to verify against
	t.Run("passes when commit is on branch", func(t *testing.T) {
		t.Setenv("GIT_AUTHOR_NAME", "Buildkite Agent")
		t.Setenv("GIT_AUTHOR_EMAIL", "agent@example.com")
		t.Setenv("GIT_COMMITTER_NAME", "Buildkite Agent")
		t.Setenv("GIT_COMMITTER_EMAIL", "agent@example.com")

		ctx := t.Context()

		s := githttptest.NewServer()
		defer s.Close()

		err := s.CreateRepository("verify-on-branch")
		if err != nil {
			t.Fatalf("CreateRepository error = %v", err)
		}

		_, err = s.InitRepository("verify-on-branch")
		if err != nil {
			t.Fatalf("InitRepository error = %v", err)
		}

		// PushBranch creates a new branch with a commit and returns the commit SHA
		commit, _, err := s.PushBranch("verify-on-branch", "feature")
		if err != nil {
			t.Fatalf("PushBranch error = %v", err)
		}

		// Clone the repo into a temp dir
		cloneDir, err := os.MkdirTemp("", "verify-commit-test-")
		if err != nil {
			t.Fatalf("MkdirTemp error = %v", err)
		}
		t.Cleanup(func() { os.RemoveAll(cloneDir) }) //nolint:errcheck // Best-effort cleanup.

		sh, err := shell.New()
		if err != nil {
			t.Fatalf("shell.New() error = %v", err)
		}

		// Clone and fetch all branches
		if err := sh.Command("git", "clone", s.RepoURL("verify-on-branch"), cloneDir).Run(ctx); err != nil {
			t.Fatalf("git clone error = %v", err)
		}
		if err := sh.Chdir(cloneDir); err != nil {
			t.Fatalf("Chdir error = %v", err)
		}
		if err := sh.Command("git", "fetch", "origin", "feature").Run(ctx); err != nil {
			t.Fatalf("git fetch error = %v", err)
		}

		e := &Executor{
			shell: sh,
			ExecutorConfig: ExecutorConfig{
				GitCommitVerification: "strict",
				Commit:                commit,
				// A plain branch name, as BUILDKITE_BRANCH always is, not a
				// pre-resolved remote-tracking ref. checkCommitOnBranch fetches it.
				Branch: "feature",
			},
		}

		// checkCommitOnBranch returns nil only when the commit is genuinely
		// verified on the branch; an unavailable check returns an error instead.
		// Asserting nil here proves verification ran, rather than silently
		// degrading to a warning (which verifyCommit would also report as nil).
		if err := e.checkCommitOnBranch(ctx); err != nil {
			t.Errorf("checkCommitOnBranch() error = %v, want nil (verified)", err)
		}
	})

	t.Run("fails when commit is not on branch", func(t *testing.T) {
		t.Setenv("GIT_AUTHOR_NAME", "Buildkite Agent")
		t.Setenv("GIT_AUTHOR_EMAIL", "agent@example.com")
		t.Setenv("GIT_COMMITTER_NAME", "Buildkite Agent")
		t.Setenv("GIT_COMMITTER_EMAIL", "agent@example.com")

		ctx := t.Context()

		s := githttptest.NewServer()
		defer s.Close()

		err := s.CreateRepository("verify-not-on-branch")
		if err != nil {
			t.Fatalf("CreateRepository error = %v", err)
		}

		_, err = s.InitRepository("verify-not-on-branch")
		if err != nil {
			t.Fatalf("InitRepository error = %v", err)
		}

		// PushBranch creates both branches from main with the same file content,
		// which produces identical commit SHAs. We need to make a unique commit
		// on feature-b so the branches genuinely diverge.
		commit, _, err := s.PushBranch("verify-not-on-branch", "feature-a")
		if err != nil {
			t.Fatalf("PushBranch(feature-a) error = %v", err)
		}

		// Clone, create feature-b manually with different content
		workDir, err := os.MkdirTemp("", "verify-commit-work-")
		if err != nil {
			t.Fatalf("MkdirTemp error = %v", err)
		}
		t.Cleanup(func() { os.RemoveAll(workDir) }) //nolint:errcheck // Best-effort cleanup.

		setupSh, err := shell.New()
		if err != nil {
			t.Fatalf("shell.New() error = %v", err)
		}
		if err := setupSh.Command("git", "clone", s.RepoURL("verify-not-on-branch"), workDir).Run(ctx); err != nil {
			t.Fatalf("git clone error = %v", err)
		}
		if err := setupSh.Chdir(workDir); err != nil {
			t.Fatalf("Chdir error = %v", err)
		}
		if err := setupSh.Command("git", "checkout", "-b", "feature-b").Run(ctx); err != nil {
			t.Fatalf("git checkout error = %v", err)
		}
		if err := os.WriteFile(filepath.Join(workDir, "unique-b.txt"), []byte("unique content for branch b"), 0o644); err != nil {
			t.Fatalf("WriteFile error = %v", err)
		}
		if err := setupSh.Command("git", "add", "unique-b.txt").Run(ctx); err != nil {
			t.Fatalf("git add error = %v", err)
		}
		if err := setupSh.Command("git", "commit", "-m", "unique commit on feature-b").Run(ctx); err != nil {
			t.Fatalf("git commit error = %v", err)
		}
		if err := setupSh.Command("git", "push", "origin", "feature-b").Run(ctx); err != nil {
			t.Fatalf("git push error = %v", err)
		}

		// Now clone fresh and verify
		cloneDir, err := os.MkdirTemp("", "verify-commit-test-")
		if err != nil {
			t.Fatalf("MkdirTemp error = %v", err)
		}
		t.Cleanup(func() { os.RemoveAll(cloneDir) }) //nolint:errcheck // Best-effort cleanup.

		sh, err := shell.New()
		if err != nil {
			t.Fatalf("shell.New() error = %v", err)
		}

		if err := sh.Command("git", "clone", s.RepoURL("verify-not-on-branch"), cloneDir).Run(ctx); err != nil {
			t.Fatalf("git clone error = %v", err)
		}
		if err := sh.Chdir(cloneDir); err != nil {
			t.Fatalf("Chdir error = %v", err)
		}
		if err := sh.Command("git", "fetch", "origin", "feature-a:refs/remotes/origin/feature-a", "feature-b:refs/remotes/origin/feature-b").Run(ctx); err != nil {
			t.Fatalf("git fetch error = %v", err)
		}

		e := &Executor{
			shell: sh,
			ExecutorConfig: ExecutorConfig{
				GitCommitVerification: "strict",
				Commit:                commit,      // commit from feature-a
				Branch:                "feature-b", // but checking against feature-b
			},
		}

		err = e.verifyCommit(ctx)
		if err == nil {
			t.Errorf("verifyCommit() error = nil, want error about commit not on branch")
		}
	})

	t.Run("warn mode logs warning but does not fail", func(t *testing.T) {
		t.Setenv("GIT_AUTHOR_NAME", "Buildkite Agent")
		t.Setenv("GIT_AUTHOR_EMAIL", "agent@example.com")
		t.Setenv("GIT_COMMITTER_NAME", "Buildkite Agent")
		t.Setenv("GIT_COMMITTER_EMAIL", "agent@example.com")

		ctx := t.Context()

		s := githttptest.NewServer()
		defer s.Close()

		err := s.CreateRepository("verify-warn-mode")
		if err != nil {
			t.Fatalf("CreateRepository error = %v", err)
		}

		_, err = s.InitRepository("verify-warn-mode")
		if err != nil {
			t.Fatalf("InitRepository error = %v", err)
		}

		commit, _, err := s.PushBranch("verify-warn-mode", "feature-a")
		if err != nil {
			t.Fatalf("PushBranch(feature-a) error = %v", err)
		}

		// Create feature-b with unique content so the branches genuinely diverge
		workDir, err := os.MkdirTemp("", "verify-commit-work-")
		if err != nil {
			t.Fatalf("MkdirTemp error = %v", err)
		}
		t.Cleanup(func() { os.RemoveAll(workDir) }) //nolint:errcheck // Best-effort cleanup.

		setupSh, err := shell.New()
		if err != nil {
			t.Fatalf("shell.New() error = %v", err)
		}
		if err := setupSh.Command("git", "clone", s.RepoURL("verify-warn-mode"), workDir).Run(ctx); err != nil {
			t.Fatalf("git clone error = %v", err)
		}
		if err := setupSh.Chdir(workDir); err != nil {
			t.Fatalf("Chdir error = %v", err)
		}
		if err := setupSh.Command("git", "checkout", "-b", "feature-b").Run(ctx); err != nil {
			t.Fatalf("git checkout error = %v", err)
		}
		if err := os.WriteFile(filepath.Join(workDir, "unique-b.txt"), []byte("unique content for branch b"), 0o644); err != nil {
			t.Fatalf("WriteFile error = %v", err)
		}
		if err := setupSh.Command("git", "add", "unique-b.txt").Run(ctx); err != nil {
			t.Fatalf("git add error = %v", err)
		}
		if err := setupSh.Command("git", "commit", "-m", "unique commit on feature-b").Run(ctx); err != nil {
			t.Fatalf("git commit error = %v", err)
		}
		if err := setupSh.Command("git", "push", "origin", "feature-b").Run(ctx); err != nil {
			t.Fatalf("git push error = %v", err)
		}

		cloneDir, err := os.MkdirTemp("", "verify-commit-test-")
		if err != nil {
			t.Fatalf("MkdirTemp error = %v", err)
		}
		t.Cleanup(func() { os.RemoveAll(cloneDir) }) //nolint:errcheck // Best-effort cleanup.

		sh, err := shell.New()
		if err != nil {
			t.Fatalf("shell.New() error = %v", err)
		}

		if err := sh.Command("git", "clone", s.RepoURL("verify-warn-mode"), cloneDir).Run(ctx); err != nil {
			t.Fatalf("git clone error = %v", err)
		}
		if err := sh.Chdir(cloneDir); err != nil {
			t.Fatalf("Chdir error = %v", err)
		}
		if err := sh.Command("git", "fetch", "origin", "feature-a:refs/remotes/origin/feature-a", "feature-b:refs/remotes/origin/feature-b").Run(ctx); err != nil {
			t.Fatalf("git fetch error = %v", err)
		}

		e := &Executor{
			shell: sh,
			ExecutorConfig: ExecutorConfig{
				GitCommitVerification: "warn",
				Commit:                commit,      // commit from feature-a
				Branch:                "feature-b", // but checking against feature-b
			},
		}

		// The underlying check must genuinely detect the failure (not merely
		// degrade to "unavailable"), or warn mode swallowing it below proves nothing.
		if err := e.checkCommitOnBranch(ctx); !errors.Is(err, ErrCommitVerificationFailed) {
			t.Fatalf("checkCommitOnBranch() error = %v, want ErrCommitVerificationFailed", err)
		}

		// In warn mode, that failure must NOT be surfaced as an error.
		if err := e.verifyCommit(ctx); err != nil {
			t.Errorf("verifyCommit() in warn mode error = %v, want nil", err)
		}
	})

	t.Run("passes after deepening a shallow clone", func(t *testing.T) {
		ctx := t.Context()
		repoURL, deepAncestor, _ := setupFileBackedRepo(t, ctx, "feature")

		cloneDir, err := os.MkdirTemp("", "verify-commit-test-")
		if err != nil {
			t.Fatalf("MkdirTemp error = %v", err)
		}
		t.Cleanup(func() { os.RemoveAll(cloneDir) }) //nolint:errcheck // Best-effort cleanup.
		sh, err := shell.New()
		if err != nil {
			t.Fatalf("shell.New() error = %v", err)
		}

		// Shallow clone with depth=1: the ancestor is beyond the boundary, so the
		// check must deepen to find it.
		if err := sh.Command("git", "clone", "--depth=1", "--branch", "feature", repoURL, cloneDir).Run(ctx); err != nil {
			t.Fatalf("git clone error = %v", err)
		}
		if err := sh.Chdir(cloneDir); err != nil {
			t.Fatalf("Chdir error = %v", err)
		}

		e := &Executor{
			shell: sh,
			ExecutorConfig: ExecutorConfig{
				GitCommitVerification: "strict",
				Commit:                deepAncestor,
				Branch:                "feature",
			},
		}

		// nil only when truly verified, so this fails if the deepen path regresses
		// to reporting the commit unavailable.
		if err := e.checkCommitOnBranch(ctx); err != nil {
			t.Errorf("checkCommitOnBranch() error = %v, want nil (verified after deepening)", err)
		}
	})

	t.Run("fails on a shallow clone when commit is not an ancestor", func(t *testing.T) {
		ctx := t.Context()
		repoURL, _, offBranchCommit := setupFileBackedRepo(t, ctx, "feature")

		cloneDir, err := os.MkdirTemp("", "verify-commit-test-")
		if err != nil {
			t.Fatalf("MkdirTemp error = %v", err)
		}
		t.Cleanup(func() { os.RemoveAll(cloneDir) }) //nolint:errcheck // Best-effort cleanup.
		sh, err := shell.New()
		if err != nil {
			t.Fatalf("shell.New() error = %v", err)
		}

		// Shallow clone of feature, then fetch the off-branch commit at depth=1 so it
		// is present locally (as the real checkout fetches BUILDKITE_COMMIT) while the
		// repo stays shallow going into checkCommitOnBranch.
		if err := sh.Command("git", "clone", "--depth=1", "--branch", "feature", repoURL, cloneDir).Run(ctx); err != nil {
			t.Fatalf("git clone error = %v", err)
		}
		if err := sh.Chdir(cloneDir); err != nil {
			t.Fatalf("Chdir error = %v", err)
		}
		if err := sh.Command("git", "fetch", "--depth=1", "origin", "other").Run(ctx); err != nil {
			t.Fatalf("git fetch (off-branch commit) error = %v", err)
		}

		e := &Executor{
			shell: sh,
			ExecutorConfig: ExecutorConfig{
				GitCommitVerification: "strict",
				Commit:                offBranchCommit,
				Branch:                "feature",
			},
		}

		// A shallow clone must not let a genuinely off-branch commit slip through:
		// deepen/unshallow, then report a definitive failure, not "unavailable".
		if err := e.checkCommitOnBranch(ctx); !errors.Is(err, ErrCommitVerificationFailed) {
			t.Errorf("checkCommitOnBranch() error = %v, want ErrCommitVerificationFailed", err)
		}
	})

	t.Run("fails on a shallow clone with a configured --depth fetch flag", func(t *testing.T) {
		ctx := t.Context()
		repoURL, _, offBranchCommit := setupFileBackedRepo(t, ctx, "feature")

		cloneDir, err := os.MkdirTemp("", "verify-commit-test-")
		if err != nil {
			t.Fatalf("MkdirTemp error = %v", err)
		}
		t.Cleanup(func() { os.RemoveAll(cloneDir) }) //nolint:errcheck // Best-effort cleanup.
		sh, err := shell.New()
		if err != nil {
			t.Fatalf("shell.New() error = %v", err)
		}

		// Same shallow off-branch setup as above: clone feature at depth=1, then
		// fetch the off-branch commit so it exists locally while the repo stays shallow.
		if err := sh.Command("git", "clone", "--depth=1", "--branch", "feature", repoURL, cloneDir).Run(ctx); err != nil {
			t.Fatalf("git clone error = %v", err)
		}
		if err := sh.Chdir(cloneDir); err != nil {
			t.Fatalf("Chdir error = %v", err)
		}
		if err := sh.Command("git", "fetch", "--depth=1", "origin", "other").Run(ctx); err != nil {
			t.Fatalf("git fetch (off-branch commit) error = %v", err)
		}

		// --depth=1 in the configured fetch flags must not reach the deepening
		// fetches: git rejects --depth alongside --deepen/--unshallow, so an
		// un-stripped flag would make the deepen fetch exit non-zero and degrade a
		// genuinely off-branch commit on a shallow clone to "unavailable" (warn,
		// never blocking under strict) instead of a definitive failure.
		e := &Executor{
			shell: sh,
			ExecutorConfig: ExecutorConfig{
				GitCommitVerification: "strict",
				Commit:                offBranchCommit,
				Branch:                "feature",
				GitFetchFlags:         "--depth=1",
			},
		}

		if err := e.checkCommitOnBranch(ctx); !errors.Is(err, ErrCommitVerificationFailed) {
			t.Errorf("checkCommitOnBranch() error = %v, want ErrCommitVerificationFailed", err)
		}
	})

	t.Run("verifies a branch whose name contains shell metacharacters", func(t *testing.T) {
		ctx := t.Context()
		// A single quote is a legal git ref character. The branch tip must be
		// fetched with the ref intact: routing it through a shell-word splitter
		// would corrupt it, and the check would silently degrade to "unavailable"
		// (warn, never blocking) instead of catching an off-branch commit.
		const branch = "quote'branch"
		repoURL, _, offBranchCommit := setupFileBackedRepo(t, ctx, branch)

		cloneDir, err := os.MkdirTemp("", "verify-commit-test-")
		if err != nil {
			t.Fatalf("MkdirTemp error = %v", err)
		}
		t.Cleanup(func() { os.RemoveAll(cloneDir) }) //nolint:errcheck // Best-effort cleanup.
		sh, err := shell.New()
		if err != nil {
			t.Fatalf("shell.New() error = %v", err)
		}

		// A full clone brings the off-branch commit's object across, so it exists
		// locally going into the check (as the real checkout fetches the commit).
		if err := sh.Command("git", "clone", "--branch", branch, repoURL, cloneDir).Run(ctx); err != nil {
			t.Fatalf("git clone error = %v", err)
		}
		if err := sh.Chdir(cloneDir); err != nil {
			t.Fatalf("Chdir error = %v", err)
		}

		e := &Executor{
			shell: sh,
			ExecutorConfig: ExecutorConfig{
				GitCommitVerification: "strict",
				Commit:                offBranchCommit,
				Branch:                branch,
			},
		}

		if err := e.checkCommitOnBranch(ctx); !errors.Is(err, ErrCommitVerificationFailed) {
			t.Errorf("checkCommitOnBranch() error = %v, want ErrCommitVerificationFailed", err)
		}
	})

	t.Run("fails when a same-named tag carries an off-branch commit", func(t *testing.T) {
		ctx := t.Context()
		repoURL, offBranchTagCommit := setupTagBranchCollisionRepo(t, ctx)

		cloneDir, err := os.MkdirTemp("", "verify-commit-test-")
		if err != nil {
			t.Fatalf("MkdirTemp error = %v", err)
		}
		t.Cleanup(func() { os.RemoveAll(cloneDir) }) //nolint:errcheck // Best-effort cleanup.
		sh, err := shell.New()
		if err != nil {
			t.Fatalf("shell.New() error = %v", err)
		}

		// A full clone brings refs/heads/release plus the refs/tags/release object,
		// so the off-branch commit exists locally going into the check.
		if err := sh.Command("git", "clone", "--branch", "release", repoURL, cloneDir).Run(ctx); err != nil {
			t.Fatalf("git clone error = %v", err)
		}
		if err := sh.Chdir(cloneDir); err != nil {
			t.Fatalf("Chdir error = %v", err)
		}

		e := &Executor{
			shell: sh,
			ExecutorConfig: ExecutorConfig{
				GitCommitVerification: "strict",
				Commit:                offBranchTagCommit,
				Branch:                "release",
			},
		}

		// The fetch must resolve refs/heads/release, not the same-named tag. If it
		// pinned FETCH_HEAD to the tag tip, this commit (the tag itself) would pass;
		// qualifying the ref makes it a definitive failure.
		if err := e.checkCommitOnBranch(ctx); !errors.Is(err, ErrCommitVerificationFailed) {
			t.Errorf("checkCommitOnBranch() error = %v, want ErrCommitVerificationFailed", err)
		}
	})

	t.Run("preserves configured git-fetch flags", func(t *testing.T) {
		ctx := t.Context()
		repoURL, _, offBranchCommit := setupFileBackedRepo(t, ctx, "feature")

		cloneDir, err := os.MkdirTemp("", "verify-commit-test-")
		if err != nil {
			t.Fatalf("MkdirTemp error = %v", err)
		}
		t.Cleanup(func() { os.RemoveAll(cloneDir) }) //nolint:errcheck // Best-effort cleanup.
		sh, err := shell.New()
		if err != nil {
			t.Fatalf("shell.New() error = %v", err)
		}
		if err := sh.Command("git", "clone", "--branch", "feature", repoURL, cloneDir).Run(ctx); err != nil {
			t.Fatalf("git clone error = %v", err)
		}
		if err := sh.Chdir(cloneDir); err != nil {
			t.Fatalf("Chdir error = %v", err)
		}

		// A bogus --upload-pack makes the branch-tip fetch fail, but only if the
		// configured git-fetch flags actually reach git fetch. If they were dropped,
		// the fetch would succeed and this off-branch commit would produce a
		// definitive ErrCommitVerificationFailed; honouring the flag makes the fetch
		// fail and degrades the check to "unavailable" instead, which is what proves
		// the flag was passed through.
		e := &Executor{
			shell: sh,
			ExecutorConfig: ExecutorConfig{
				GitCommitVerification: "strict",
				Commit:                offBranchCommit,
				Branch:                "feature",
				GitFetchFlags:         "--upload-pack=/nonexistent/git-upload-pack",
			},
		}

		if err := e.checkCommitOnBranch(ctx); !errors.Is(err, ErrCommitVerificationUnavailable) {
			t.Errorf("checkCommitOnBranch() error = %v, want ErrCommitVerificationUnavailable", err)
		}
	})

	t.Run("resolves the branch tip even with an --append fetch flag", func(t *testing.T) {
		ctx := t.Context()
		repoURL, _, offBranchCommit := setupFileBackedRepo(t, ctx, "feature")

		cloneDir, err := os.MkdirTemp("", "verify-commit-test-")
		if err != nil {
			t.Fatalf("MkdirTemp error = %v", err)
		}
		t.Cleanup(func() { os.RemoveAll(cloneDir) }) //nolint:errcheck // Best-effort cleanup.
		sh, err := shell.New()
		if err != nil {
			t.Fatalf("shell.New() error = %v", err)
		}
		if err := sh.Command("git", "clone", "--branch", "feature", repoURL, cloneDir).Run(ctx); err != nil {
			t.Fatalf("git clone error = %v", err)
		}
		if err := sh.Chdir(cloneDir); err != nil {
			t.Fatalf("Chdir error = %v", err)
		}

		// Fetch the off-branch commit so it exists locally AND leaves FETCH_HEAD
		// pointing at it, mirroring fetchSource recording the build commit before
		// verification runs.
		if err := sh.Command("git", "fetch", "origin", "other").Run(ctx); err != nil {
			t.Fatalf("git fetch (off-branch commit) error = %v", err)
		}

		// --append makes git append the branch fetch to FETCH_HEAD rather than
		// overwriting it, so rev-parse FETCH_HEAD would resolve to the earlier entry
		// (the off-branch build commit) and merge-base <commit> <commit> would pass.
		// Resolving a dedicated ref instead pins the real branch tip and catches the
		// off-branch commit.
		e := &Executor{
			shell: sh,
			ExecutorConfig: ExecutorConfig{
				GitCommitVerification: "strict",
				Commit:                offBranchCommit,
				Branch:                "feature",
				GitFetchFlags:         "--append",
			},
		}

		if err := e.checkCommitOnBranch(ctx); !errors.Is(err, ErrCommitVerificationFailed) {
			t.Errorf("checkCommitOnBranch() error = %v, want ErrCommitVerificationFailed", err)
		}
	})

	t.Run("fails when a configured fetch mode would suppress the ref update", func(t *testing.T) {
		// --dry-run writes no ref and --prefetch redirects it under refs/prefetch/;
		// either would leave the branch-tip ref unresolvable and degrade the check
		// to "unavailable" (a pass under strict). They must be stripped so the tip
		// is actually pinned and the off-branch commit is caught. The abbreviated
		// spellings git also accepts (--dry, --prefe) must be caught as well.
		for _, flag := range []string{"--dry-run", "--dry", "--prefetch", "--prefe"} {
			t.Run(flag, func(t *testing.T) {
				ctx := t.Context()
				repoURL, _, offBranchCommit := setupFileBackedRepo(t, ctx, "feature")

				cloneDir, err := os.MkdirTemp("", "verify-commit-test-")
				if err != nil {
					t.Fatalf("MkdirTemp error = %v", err)
				}
				t.Cleanup(func() { os.RemoveAll(cloneDir) }) //nolint:errcheck // Best-effort cleanup.
				sh, err := shell.New()
				if err != nil {
					t.Fatalf("shell.New() error = %v", err)
				}
				if err := sh.Command("git", "clone", "--branch", "feature", repoURL, cloneDir).Run(ctx); err != nil {
					t.Fatalf("git clone error = %v", err)
				}
				if err := sh.Chdir(cloneDir); err != nil {
					t.Fatalf("Chdir error = %v", err)
				}
				// Bring the off-branch commit across so it exists locally, as the real
				// checkout's fetchSource would.
				if err := sh.Command("git", "fetch", "origin", "other").Run(ctx); err != nil {
					t.Fatalf("git fetch (off-branch commit) error = %v", err)
				}

				e := &Executor{
					shell: sh,
					ExecutorConfig: ExecutorConfig{
						GitCommitVerification: "strict",
						Commit:                offBranchCommit,
						Branch:                "feature",
						GitFetchFlags:         flag,
					},
				}

				if err := e.checkCommitOnBranch(ctx); !errors.Is(err, ErrCommitVerificationFailed) {
					t.Errorf("checkCommitOnBranch() error = %v, want ErrCommitVerificationFailed", err)
				}
			})
		}
	})

	t.Run("verifies via --unshallow beyond the deepen boundary", func(t *testing.T) {
		ctx := t.Context()
		sh, repoURL, commit := newFileBackedRepo(t, ctx, "verify-unshallow")

		// Put the target at the root, then pile more than (1 + --deepen=50) commits
		// on top so neither the depth=1 clone nor the first --deepen=50 reaches it,
		// forcing the check all the way to the --unshallow iteration.
		deepTarget := commit("root.txt")
		for range 60 {
			if err := sh.Command("git", "commit", "--allow-empty", "-m", "filler").Run(ctx); err != nil {
				t.Fatalf("git commit --allow-empty error = %v", err)
			}
		}
		if err := sh.Command("git", "branch", "-m", "deep").Run(ctx); err != nil {
			t.Fatalf("git branch -m deep error = %v", err)
		}
		if err := sh.Command("git", "push", "origin", "deep").Run(ctx); err != nil {
			t.Fatalf("git push deep error = %v", err)
		}

		cloneDir, err := os.MkdirTemp("", "verify-commit-test-")
		if err != nil {
			t.Fatalf("MkdirTemp error = %v", err)
		}
		t.Cleanup(func() { os.RemoveAll(cloneDir) }) //nolint:errcheck // Best-effort cleanup.
		clone, err := shell.New()
		if err != nil {
			t.Fatalf("shell.New() error = %v", err)
		}
		if err := clone.Command("git", "clone", "--depth=1", "--branch", "deep", repoURL, cloneDir).Run(ctx); err != nil {
			t.Fatalf("git clone error = %v", err)
		}
		if err := clone.Chdir(cloneDir); err != nil {
			t.Fatalf("Chdir error = %v", err)
		}

		e := &Executor{
			shell: clone,
			ExecutorConfig: ExecutorConfig{
				GitCommitVerification: "strict",
				Commit:                deepTarget,
				Branch:                "deep",
			},
		}

		// --deepen=50 can't reach a commit 60 back, so a nil result proves the
		// --unshallow iteration is what verified it.
		if err := e.checkCommitOnBranch(ctx); err != nil {
			t.Errorf("checkCommitOnBranch() error = %v, want nil (verified via --unshallow)", err)
		}
	})

	t.Run("does not skip a non-PR build", func(t *testing.T) {
		ctx := t.Context()
		// BUILDKITE_PULL_REQUEST is the string "false" (not empty) on ordinary
		// builds; only a real PR number should skip verification. Point at an
		// off-branch commit and prove the check still runs and fails rather than
		// being skipped by the "false" sentinel.
		repoURL, _, offBranchCommit := setupFileBackedRepo(t, ctx, "feature")

		cloneDir, err := os.MkdirTemp("", "verify-commit-test-")
		if err != nil {
			t.Fatalf("MkdirTemp error = %v", err)
		}
		t.Cleanup(func() { os.RemoveAll(cloneDir) }) //nolint:errcheck // Best-effort cleanup.
		sh, err := shell.New()
		if err != nil {
			t.Fatalf("shell.New() error = %v", err)
		}
		if err := sh.Command("git", "clone", "--branch", "feature", repoURL, cloneDir).Run(ctx); err != nil {
			t.Fatalf("git clone error = %v", err)
		}
		if err := sh.Chdir(cloneDir); err != nil {
			t.Fatalf("Chdir error = %v", err)
		}

		e := &Executor{
			shell: sh,
			ExecutorConfig: ExecutorConfig{
				GitCommitVerification: "strict",
				Commit:                offBranchCommit,
				Branch:                "feature",
				PullRequest:           "false",
			},
		}

		// A skip would return nil; verifyCommit must surface the failure instead.
		if err := e.verifyCommit(ctx); !errors.Is(err, ErrCommitVerificationFailed) {
			t.Errorf("verifyCommit() error = %v, want ErrCommitVerificationFailed (a non-PR build must not be skipped)", err)
		}
	})

	t.Run("unavailable check does not fail strict mode", func(t *testing.T) {
		t.Setenv("GIT_AUTHOR_NAME", "Buildkite Agent")
		t.Setenv("GIT_AUTHOR_EMAIL", "agent@example.com")
		t.Setenv("GIT_COMMITTER_NAME", "Buildkite Agent")
		t.Setenv("GIT_COMMITTER_EMAIL", "agent@example.com")

		ctx := t.Context()

		s := githttptest.NewServer()
		defer s.Close()

		if err := s.CreateRepository("verify-unavailable"); err != nil {
			t.Fatalf("CreateRepository error = %v", err)
		}
		if _, err := s.InitRepository("verify-unavailable"); err != nil {
			t.Fatalf("InitRepository error = %v", err)
		}
		commit, _, err := s.PushBranch("verify-unavailable", "feature")
		if err != nil {
			t.Fatalf("PushBranch error = %v", err)
		}

		cloneDir, err := os.MkdirTemp("", "verify-commit-test-")
		if err != nil {
			t.Fatalf("MkdirTemp error = %v", err)
		}
		t.Cleanup(func() { os.RemoveAll(cloneDir) }) //nolint:errcheck // Best-effort cleanup.
		sh, err := shell.New()
		if err != nil {
			t.Fatalf("shell.New() error = %v", err)
		}
		if err := sh.Command("git", "clone", s.RepoURL("verify-unavailable"), cloneDir).Run(ctx); err != nil {
			t.Fatalf("git clone error = %v", err)
		}
		if err := sh.Chdir(cloneDir); err != nil {
			t.Fatalf("Chdir error = %v", err)
		}

		// A branch the remote doesn't have can't be fetched, so ancestry can't be
		// checked. That is an infrastructure failure, not evidence of an attack.
		e := &Executor{
			shell: sh,
			ExecutorConfig: ExecutorConfig{
				GitCommitVerification: "strict",
				Commit:                commit,
				Branch:                "does-not-exist",
			},
		}

		if err := e.checkCommitOnBranch(ctx); !errors.Is(err, ErrCommitVerificationUnavailable) {
			t.Fatalf("checkCommitOnBranch() error = %v, want ErrCommitVerificationUnavailable", err)
		}

		// Even in strict mode, an unavailable check must not fail the build.
		if err := e.verifyCommit(ctx); err != nil {
			t.Errorf("verifyCommit() in strict mode with unavailable check error = %v, want nil", err)
		}
	})
}

func TestStripShallowFetchFlags(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"nil", nil, []string{}},
		{"transport flags survive", []string{"-v", "--prune", "--upload-pack=/x"}, []string{"-v", "--prune", "--upload-pack=/x"}},
		{"depth equals form", []string{"--depth=1"}, []string{}},
		{"depth space form drops the value token", []string{"--depth", "1"}, []string{}},
		{"deepen and unshallow", []string{"--deepen=50", "--unshallow"}, []string{}},
		{"shallow-since and shallow-exclude", []string{"--shallow-since=2020-01-01", "--shallow-exclude", "v1.0"}, []string{}},
		{"keeps surrounding flags", []string{"-v", "--depth=1", "--prune"}, []string{"-v", "--prune"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripShallowFetchFlags(tt.in)
			if !slices.Equal(got, tt.want) {
				t.Errorf("stripShallowFetchFlags(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestStripRefSuppressingFetchFlags(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"nil", nil, []string{}},
		{"transport flags survive", []string{"-v", "--prune", "--upload-pack=/x"}, []string{"-v", "--prune", "--upload-pack=/x"}},
		{"dry-run", []string{"--dry-run"}, []string{}},
		{"prefetch", []string{"--prefetch"}, []string{}},
		{"negotiate-only", []string{"--negotiate-only"}, []string{}},
		{"dry-run abbreviation", []string{"--dry"}, []string{}},
		{"prefetch abbreviation", []string{"--prefe"}, []string{}},
		{"negotiate-only abbreviation", []string{"--negotiate"}, []string{}},
		{"-n is --no-tags, not dry-run, so it survives", []string{"-n"}, []string{"-n"}},
		{"--prune is not a prefix of these modes", []string{"--prune"}, []string{"--prune"}},
		{"--negotiation-tip survives", []string{"--negotiation-tip=abc"}, []string{"--negotiation-tip=abc"}},
		{"--no-* negations survive", []string{"--no-dry-run", "--no-prefetch"}, []string{"--no-dry-run", "--no-prefetch"}},
		{"keeps surrounding flags", []string{"-v", "--dry-run", "--prune"}, []string{"-v", "--prune"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripRefSuppressingFetchFlags(tt.in)
			if !slices.Equal(got, tt.want) {
				t.Errorf("stripRefSuppressingFetchFlags(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
