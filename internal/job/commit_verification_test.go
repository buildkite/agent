package job

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/internal/job/githttptest"
	"github.com/buildkite/agent/v3/internal/shell"
)

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
		t.Setenv("GIT_AUTHOR_NAME", "Buildkite Agent")
		t.Setenv("GIT_AUTHOR_EMAIL", "agent@example.com")
		t.Setenv("GIT_COMMITTER_NAME", "Buildkite Agent")
		t.Setenv("GIT_COMMITTER_EMAIL", "agent@example.com")

		ctx := t.Context()

		s := githttptest.NewServer()
		defer s.Close()

		err := s.CreateRepository("verify-shallow")
		if err != nil {
			t.Fatalf("CreateRepository error = %v", err)
		}

		_, err = s.InitRepository("verify-shallow")
		if err != nil {
			t.Fatalf("InitRepository error = %v", err)
		}

		// InitRepository puts the initial commit on main; PushBranch adds one more
		// commit on feature. The initial commit is an ancestor of feature but sits
		// beyond a depth=1 clone's boundary, so verifying it forces the deepen path.
		if _, _, err := s.PushBranch("verify-shallow", "feature"); err != nil {
			t.Fatalf("PushBranch error = %v", err)
		}

		// Read the initial commit SHA (the deep ancestor) from a throwaway full clone.
		refDir, err := os.MkdirTemp("", "verify-commit-ref-")
		if err != nil {
			t.Fatalf("MkdirTemp error = %v", err)
		}
		t.Cleanup(func() { os.RemoveAll(refDir) }) //nolint:errcheck // Best-effort cleanup.
		refSh, err := shell.New()
		if err != nil {
			t.Fatalf("shell.New() error = %v", err)
		}
		if err := refSh.Command("git", "clone", s.RepoURL("verify-shallow"), refDir).Run(ctx); err != nil {
			t.Fatalf("git clone error = %v", err)
		}
		if err := refSh.Chdir(refDir); err != nil {
			t.Fatalf("Chdir error = %v", err)
		}
		deepCommit, err := refSh.Command("git", "rev-parse", "origin/main").RunAndCaptureStdout(ctx)
		if err != nil {
			t.Fatalf("rev-parse origin/main error = %v", err)
		}
		deepCommit = strings.TrimSpace(deepCommit)

		cloneDir, err := os.MkdirTemp("", "verify-commit-test-")
		if err != nil {
			t.Fatalf("MkdirTemp error = %v", err)
		}
		t.Cleanup(func() { os.RemoveAll(cloneDir) }) //nolint:errcheck // Best-effort cleanup.

		sh, err := shell.New()
		if err != nil {
			t.Fatalf("shell.New() error = %v", err)
		}

		// Shallow clone with depth=1: the deep ancestor is beyond the boundary.
		if err := sh.Command("git", "clone", "--depth=1", "--branch", "feature", s.RepoURL("verify-shallow"), cloneDir).Run(ctx); err != nil {
			t.Fatalf("git clone error = %v", err)
		}
		if err := sh.Chdir(cloneDir); err != nil {
			t.Fatalf("Chdir error = %v", err)
		}

		// The commit is a genuine ancestor of feature, but not visible until the
		// shallow clone is deepened. verifyCommit must deepen rather than trust the
		// initial shallow "not an ancestor" result.
		e := &Executor{
			shell: sh,
			ExecutorConfig: ExecutorConfig{
				GitCommitVerification: "strict",
				Commit:                deepCommit,
				Branch:                "feature",
			},
		}

		// Assert on checkCommitOnBranch (nil only when truly verified) so the test
		// fails if the deepen path regresses to reporting the commit unavailable.
		if err := e.checkCommitOnBranch(ctx); err != nil {
			t.Errorf("checkCommitOnBranch() error = %v, want nil (verified after deepening)", err)
		}
	})

	t.Run("fails on a shallow clone when commit is not an ancestor", func(t *testing.T) {
		t.Setenv("GIT_AUTHOR_NAME", "Buildkite Agent")
		t.Setenv("GIT_AUTHOR_EMAIL", "agent@example.com")
		t.Setenv("GIT_COMMITTER_NAME", "Buildkite Agent")
		t.Setenv("GIT_COMMITTER_EMAIL", "agent@example.com")

		ctx := t.Context()

		s := githttptest.NewServer()
		defer s.Close()

		if err := s.CreateRepository("verify-shallow-fail"); err != nil {
			t.Fatalf("CreateRepository error = %v", err)
		}
		if _, err := s.InitRepository("verify-shallow-fail"); err != nil {
			t.Fatalf("InitRepository error = %v", err)
		}
		if _, _, err := s.PushBranch("verify-shallow-fail", "feature"); err != nil {
			t.Fatalf("PushBranch error = %v", err)
		}

		// Build a commit on "other" that diverges from feature, so it is genuinely
		// not reachable from it. Deepening feature never brings this commit in, so
		// the shallow clone below must fetch it directly (as the real checkout does
		// for BUILDKITE_COMMIT) for the ancestry check to resolve to a definitive
		// "not on branch" instead of "unavailable".
		workDir, err := os.MkdirTemp("", "verify-commit-work-")
		if err != nil {
			t.Fatalf("MkdirTemp error = %v", err)
		}
		t.Cleanup(func() { os.RemoveAll(workDir) }) //nolint:errcheck // Best-effort cleanup.
		setupSh, err := shell.New()
		if err != nil {
			t.Fatalf("shell.New() error = %v", err)
		}
		if err := setupSh.Command("git", "clone", s.RepoURL("verify-shallow-fail"), workDir).Run(ctx); err != nil {
			t.Fatalf("git clone error = %v", err)
		}
		if err := setupSh.Chdir(workDir); err != nil {
			t.Fatalf("Chdir error = %v", err)
		}
		if err := setupSh.Command("git", "checkout", "-b", "other").Run(ctx); err != nil {
			t.Fatalf("git checkout error = %v", err)
		}
		if err := os.WriteFile(filepath.Join(workDir, "unique-other.txt"), []byte("unique content on other"), 0o644); err != nil {
			t.Fatalf("WriteFile error = %v", err)
		}
		if err := setupSh.Command("git", "add", "unique-other.txt").Run(ctx); err != nil {
			t.Fatalf("git add error = %v", err)
		}
		if err := setupSh.Command("git", "commit", "-m", "unique commit on other").Run(ctx); err != nil {
			t.Fatalf("git commit error = %v", err)
		}
		if err := setupSh.Command("git", "push", "origin", "other").Run(ctx); err != nil {
			t.Fatalf("git push error = %v", err)
		}
		offBranchCommit, err := setupSh.Command("git", "rev-parse", "HEAD").RunAndCaptureStdout(ctx)
		if err != nil {
			t.Fatalf("rev-parse HEAD error = %v", err)
		}
		offBranchCommit = strings.TrimSpace(offBranchCommit)

		cloneDir, err := os.MkdirTemp("", "verify-commit-test-")
		if err != nil {
			t.Fatalf("MkdirTemp error = %v", err)
		}
		t.Cleanup(func() { os.RemoveAll(cloneDir) }) //nolint:errcheck // Best-effort cleanup.
		sh, err := shell.New()
		if err != nil {
			t.Fatalf("shell.New() error = %v", err)
		}

		// Shallow clone of feature, then fetch the off-branch commit at depth=1 so
		// the repo stays shallow going into checkCommitOnBranch.
		if err := sh.Command("git", "clone", "--depth=1", "--branch", "feature", s.RepoURL("verify-shallow-fail"), cloneDir).Run(ctx); err != nil {
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
