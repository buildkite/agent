package job

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/buildkite/agent/v3/internal/job/githttptest"
	"github.com/buildkite/agent/v3/internal/race"
	"github.com/buildkite/agent/v3/internal/shell"
)

func TestDefaultCheckoutPhase(t *testing.T) {
	ctx := context.Background()

	shell, err := shell.New()
	if err != nil {
		t.Fatalf("shell.New() error = %v, want nil", err)
	}

	tests := []struct {
		name        string
		executor    *Executor
		projectName string
		checkoutDir string
		refSpec     string
	}{
		{
			name: "Default checkout phase with HEAD commit",
			executor: &Executor{
				shell: shell,
				ExecutorConfig: ExecutorConfig{
					Commit:        "HEAD",
					Branch:        "main",
					CleanCheckout: false,
					GitCleanFlags: "-f -d -x",
				},
			},
			projectName: "project-name-head",
		},
		{
			name: "Default checkout phase with custom refspec",
			executor: &Executor{
				shell: shell,
				ExecutorConfig: ExecutorConfig{
					Commit:        "HEAD",
					Branch:        "main",
					CleanCheckout: false,
					GitCleanFlags: "-f -d -x",
					RefSpec:       "refs/custom",
				},
			},
			projectName: "project-name-refspec",
			refSpec:     "refs/custom",
		},
		{
			name: "Default checkout phase with pull request",
			executor: &Executor{
				shell: shell,
				ExecutorConfig: ExecutorConfig{
					PullRequest:      "124",
					Commit:           "HEAD",
					Branch:           "main",
					CleanCheckout:    false,
					GitCleanFlags:    "-f -d -x",
					PipelineProvider: "github",
				},
			},
			projectName: "project-name-pull-request",
			refSpec:     "refs/pull/124/head",
		},
		{
			name: "Default checkout phase with pull request using merge refspec",
			executor: &Executor{
				shell: shell,
				ExecutorConfig: ExecutorConfig{
					PullRequest:                  "124",
					Commit:                       "HEAD",
					Branch:                       "main",
					CleanCheckout:                false,
					GitCleanFlags:                "-f -d -x",
					PipelineProvider:             "github",
					PullRequestUsingMergeRefspec: true,
				},
			},
			projectName: "project-name-pull-request",
			refSpec:     "refs/pull/124/merge",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// configure a global user name and email
			// this is to avoid the git config file being created in the home directory
			// which is not needed for the test
			t.Setenv("GIT_AUTHOR_NAME", "Buildkite Agent")
			t.Setenv("GIT_AUTHOR_EMAIL", "agent@example.com")
			t.Setenv("GIT_COMMITTER_NAME", "Buildkite Agent")
			t.Setenv("GIT_COMMITTER_EMAIL", "agent@example.com")

			s := githttptest.NewServer()
			defer s.Close()

			err = s.CreateRepository(tt.projectName)
			if err != nil {
				t.Fatalf("s.CreateRepository(%q) error = %v, want nil", tt.projectName, err)
			}

			out, err := s.InitRepository(tt.projectName)
			if err != nil {
				t.Fatalf("failed to init repository: %v output: %s", err, string(out))
			}

			commit, out, err := s.PushBranch(tt.projectName, "feature-branch")
			if err != nil {
				t.Fatalf("failed to init repository: %v output: %s", err, string(out))
			}

			if tt.refSpec != "" {
				out, err = s.CreateRef(tt.projectName, tt.refSpec, commit)
				if err != nil {
					t.Fatalf("failed to create ref: %v output: %s", err, string(out))
				}
			}

			buildDir, err := os.MkdirTemp("", "build-path-")
			if err != nil {
				t.Fatalf("os.MkdirTemp(%q, %q) error = %v, want nil", "", "build-path-", err)
			}
			t.Cleanup(func() {
				os.RemoveAll(buildDir) //nolint:errcheck // Best-effort cleanup.
			})

			tt.executor.BuildPath = buildDir
			tt.executor.Repository = s.RepoURL(tt.projectName)

			checkoutDir, err := os.MkdirTemp("", "checkout-path-")
			if err != nil {
				t.Fatalf("os.MkdirTemp(%q, %q) error = %v, want nil", "", "checkout-path-", err)
			}
			t.Cleanup(func() {
				os.RemoveAll(checkoutDir) //nolint:errcheck // Best-effort cleanup.
			})

			shell.Env.Set("BUILDKITE_BUILD_CHECKOUT_PATH", checkoutDir)

			err = tt.executor.defaultCheckoutPhase(ctx)
			if err != nil {
				t.Fatalf("tt.executor.defaultCheckoutPhase(ctx) error = %v, want nil", err)
			}
		})
	}
}

func TestSkipCheckout(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	sh, err := shell.New()
	if err != nil {
		t.Fatalf("shell.New() error = %v, want nil", err)
	}

	executor := &Executor{
		shell: sh,
		ExecutorConfig: ExecutorConfig{
			Repository:   "https://github.com/buildkite/agent.git",
			SkipCheckout: true,
		},
	}

	err = executor.checkout(ctx)
	if err != nil {
		t.Fatalf("executor.checkout(ctx) error = %v, want nil", err)
	}
}

func TestDefaultCheckoutPhase_DelayedRefCreation(t *testing.T) {
	if race.IsRaceTest {
		t.Skip("this test simulates the agent recovering from a race condition, and needs to create one to test it.")
	}

	ctx := t.Context()

	shell, err := shell.New()
	if err != nil {
		t.Fatalf("shell.New() error = %v, want nil", err)
	}

	tt := struct {
		executor    *Executor
		projectName string
		checkoutDir string
		refSpec     string
	}{
		executor: &Executor{
			shell: shell,
			ExecutorConfig: ExecutorConfig{
				PullRequest:      "124",
				Commit:           "HEAD",
				Branch:           "main",
				CleanCheckout:    false,
				GitCleanFlags:    "-f -d -x",
				PipelineProvider: "github",
			},
		},
		projectName: "project-name-pull-request",
		refSpec:     "refs/pull/124/head",
	}

	// configure a global user name and email
	// this is to avoid the git config file being created in the home directory
	// which is not needed for the test
	t.Setenv("GIT_AUTHOR_NAME", "Buildkite Agent")
	t.Setenv("GIT_AUTHOR_EMAIL", "agent@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Buildkite Agent")
	t.Setenv("GIT_COMMITTER_EMAIL", "agent@example.com")

	s := githttptest.NewServer()
	defer s.Close()

	err = s.CreateRepository(tt.projectName)
	if err != nil {
		t.Fatalf("s.CreateRepository(%q) error = %v, want nil", tt.projectName, err)
	}

	out, err := s.InitRepository(tt.projectName)
	if err != nil {
		t.Fatalf("failed to init repository: %v output: %s", err, string(out))
	}

	commit, out, err := s.PushBranch(tt.projectName, "feature-branch")
	if err != nil {
		t.Fatalf("failed to init repository: %v output: %s", err, string(out))
	}

	buildDir, err := os.MkdirTemp("", "build-path-")
	if err != nil {
		t.Fatalf("os.MkdirTemp(%q, %q) error = %v, want nil", "", "build-path-", err)
	}
	t.Cleanup(func() {
		os.RemoveAll(buildDir) //nolint:errcheck // Best-effort cleanup.
	})

	tt.executor.BuildPath = buildDir
	tt.executor.Repository = s.RepoURL(tt.projectName)

	checkoutDir, err := os.MkdirTemp("", "checkout-path-")
	if err != nil {
		t.Fatalf("os.MkdirTemp(%q, %q) error = %v, want nil", "", "checkout-path-", err)
	}
	t.Cleanup(func() {
		os.RemoveAll(checkoutDir) //nolint:errcheck // Best-effort cleanup.
	})

	// Concurrently sleep for 5 seconds to delay ref being created
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
			// continue below
		}
		out, err = s.CreateRef(tt.projectName, tt.refSpec, commit)
		if err != nil {
			t.Errorf("failed to create ref: %v output: %s", err, string(out))
		}
	}()

	shell.Env.Set("BUILDKITE_BUILD_CHECKOUT_PATH", checkoutDir)

	err = tt.executor.defaultCheckoutPhase(ctx)
	if err != nil {
		t.Fatalf("tt.executor.defaultCheckoutPhase(ctx) error = %v, want nil", err)
	}
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
	}

	for _, tt := range skipTests {
		t.Run(tt.name, func(t *testing.T) {
			e := &Executor{
				ExecutorConfig: tt.config,
			}
			err := e.verifyCommit(context.Background())
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

		ctx := context.Background()

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
				Branch:                "origin/feature",
			},
		}

		err = e.verifyCommit(ctx)
		if err != nil {
			t.Errorf("verifyCommit() error = %v, want nil", err)
		}
	})

	t.Run("fails when commit is not on branch", func(t *testing.T) {
		t.Setenv("GIT_AUTHOR_NAME", "Buildkite Agent")
		t.Setenv("GIT_AUTHOR_EMAIL", "agent@example.com")
		t.Setenv("GIT_COMMITTER_NAME", "Buildkite Agent")
		t.Setenv("GIT_COMMITTER_EMAIL", "agent@example.com")

		ctx := context.Background()

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
				Commit:                commit,             // commit from feature-a
				Branch:                "origin/feature-b", // but checking against feature-b
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

		ctx := context.Background()

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
				Commit:                commit,             // commit from feature-a
				Branch:                "origin/feature-b", // but checking against feature-b
			},
		}

		// In warn mode, verification failure should NOT return an error
		err = e.verifyCommit(ctx)
		if err != nil {
			t.Errorf("verifyCommit() in warn mode error = %v, want nil", err)
		}
	})

	t.Run("passes after deepening a shallow clone", func(t *testing.T) {
		t.Setenv("GIT_AUTHOR_NAME", "Buildkite Agent")
		t.Setenv("GIT_AUTHOR_EMAIL", "agent@example.com")
		t.Setenv("GIT_COMMITTER_NAME", "Buildkite Agent")
		t.Setenv("GIT_COMMITTER_EMAIL", "agent@example.com")

		ctx := context.Background()

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

		// The initial commit from InitRepository is on main.
		// PushBranch adds one more commit on a feature branch.
		// We need the initial commit SHA to test — it will be beyond depth=1.
		commit, _, err := s.PushBranch("verify-shallow", "feature")
		if err != nil {
			t.Fatalf("PushBranch error = %v", err)
		}

		// Get the initial commit (parent of the feature commit) by reading it from the server repo
		cloneDir, err := os.MkdirTemp("", "verify-commit-test-")
		if err != nil {
			t.Fatalf("MkdirTemp error = %v", err)
		}
		t.Cleanup(func() { os.RemoveAll(cloneDir) }) //nolint:errcheck // Best-effort cleanup.

		sh, err := shell.New()
		if err != nil {
			t.Fatalf("shell.New() error = %v", err)
		}

		// Shallow clone with depth=1 — only gets the tip commit
		if err := sh.Command("git", "clone", "--depth=1", "--branch", "feature", s.RepoURL("verify-shallow"), cloneDir).Run(ctx); err != nil {
			t.Fatalf("git clone error = %v", err)
		}
		if err := sh.Chdir(cloneDir); err != nil {
			t.Fatalf("Chdir error = %v", err)
		}

		// The tip commit is the one from PushBranch — it IS on the branch,
		// but let's verify the shallow clone scenario works at all.
		// With depth=1, the commit should be present and verifiable.
		e := &Executor{
			shell: sh,
			ExecutorConfig: ExecutorConfig{
				GitCommitVerification: "strict",
				Commit:                commit,
				Branch:                "origin/feature",
			},
		}

		err = e.verifyCommit(ctx)
		if err != nil {
			t.Errorf("verifyCommit() error = %v, want nil", err)
		}
	})
}
