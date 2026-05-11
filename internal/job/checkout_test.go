package job

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func TestDefaultCheckoutPhase_GitLFS(t *testing.T) {
	// Not parallel: subtests manipulate PATH via t.Setenv, which modifies
	// process-global state.
	ctx := context.Background()

	t.Setenv("GIT_AUTHOR_NAME", "Buildkite Agent")
	t.Setenv("GIT_AUTHOR_EMAIL", "agent@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Buildkite Agent")
	t.Setenv("GIT_COMMITTER_EMAIL", "agent@example.com")

	// Note:  "LFS enabled" test bypasses GIT_LFS_SKIP_SMUDGE=1
	// setUp sets GIT_LFS_SKIP_SMUDGE=1 to prevent implicit LFS downloads
	// during git checkout. These tests call defaultCheckoutPhase directly so they
	// don't exercise that env var, but the test repo has no LFS-tracked files so
	// smudge filters are never triggered.
	tests := []struct {
		name       string
		lfsEnabled bool
		// setupPath runs before the shell and executor are created. Use it to
		// manipulate PATH for binary-presence tests.
		setupPath func(t *testing.T)
		wantErr   string
	}{
		{
			name:       "LFS disabled",
			lfsEnabled: false,
		},
		{
			name:       "LFS enabled binary present",
			lfsEnabled: true,
			setupPath: func(t *testing.T) {
				if _, err := exec.LookPath("git-lfs"); err != nil {
					t.Skip("git-lfs not installed")
				}
			},
		},
		{
			name:       "LFS enabled binary missing",
			lfsEnabled: true,
			setupPath: func(t *testing.T) {
				gitBin, err := exec.LookPath("git")
				if err != nil {
					t.Fatalf("exec.LookPath(\"git\") error = %v", err)
				}
				binDir := t.TempDir()
				if err := os.Symlink(gitBin, filepath.Join(binDir, "git")); err != nil {
					t.Fatalf("os.Symlink() error = %v", err)
				}
				t.Setenv("PATH", binDir)
			},
			wantErr: "git-lfs binary is not found on PATH",
		},
		{
			name:       "LFS enabled git lfs command fails",
			lfsEnabled: true,
			setupPath: func(t *testing.T) {
				gitBin, err := exec.LookPath("git")
				if err != nil {
					t.Fatalf("exec.LookPath(\"git\") error = %v", err)
				}
				binDir := t.TempDir()
				if err := os.Symlink(gitBin, filepath.Join(binDir, "git")); err != nil {
					t.Fatalf("os.Symlink() error = %v", err)
				}
				fakeLFS := filepath.Join(binDir, "git-lfs")
				if err := os.WriteFile(fakeLFS, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
					t.Fatalf("os.WriteFile() error = %v", err)
				}
				t.Setenv("PATH", binDir)
			},
			wantErr: "installing git lfs filter",
		},
		{
			name:       "LFS enabled git lfs fetch fails",
			lfsEnabled: true,
			setupPath: func(t *testing.T) {
				gitBin, err := exec.LookPath("git")
				if err != nil {
					t.Fatalf("exec.LookPath(\"git\") error = %v", err)
				}
				binDir := t.TempDir()
				if err := os.Symlink(gitBin, filepath.Join(binDir, "git")); err != nil {
					t.Fatalf("os.Symlink() error = %v", err)
				}
				// install exits 0; every other subcommand (fetch, checkout) exits 1.
				fakeLFS := filepath.Join(binDir, "git-lfs")
				script := "#!/bin/sh\ncase \"$1\" in\n  install) exit 0 ;;\n  *) exit 1 ;;\nesac\n"
				if err := os.WriteFile(fakeLFS, []byte(script), 0o755); err != nil {
					t.Fatalf("os.WriteFile() error = %v", err)
				}
				t.Setenv("PATH", binDir)
			},
			wantErr: "git lfs fetch",
		},
	}

	s := githttptest.NewServer()
	t.Cleanup(s.Close)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setupPath != nil {
				tt.setupPath(t)
			}

			sh, err := shell.New()
			if err != nil {
				t.Fatalf("shell.New() error = %v", err)
			}

			projectName := "lfs-" + strings.ReplaceAll(strings.ToLower(tt.name), " ", "-")
			if err := s.CreateRepository(projectName); err != nil {
				t.Fatalf("s.CreateRepository(%q) error = %v", projectName, err)
			}
			out, err := s.InitRepository(projectName)
			if err != nil {
				t.Fatalf("s.InitRepository(%q) error = %v, output: %s", projectName, err, out)
			}

			checkoutDir := t.TempDir()
			sh.Env.Set("BUILDKITE_BUILD_CHECKOUT_PATH", checkoutDir)

			executor := &Executor{
				shell: sh,
				ExecutorConfig: ExecutorConfig{
					Commit:        "HEAD",
					Branch:        "main",
					GitCleanFlags: "-f -d -x",
					BuildPath:     t.TempDir(),
					Repository:    s.RepoURL(projectName),
					GitLFSEnabled: tt.lfsEnabled,
				},
			}

			err = executor.defaultCheckoutPhase(ctx)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("defaultCheckoutPhase() error = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Errorf("defaultCheckoutPhase() error = nil, want error containing %q", tt.wantErr)
				return
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("defaultCheckoutPhase() error = %q, want it to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}
