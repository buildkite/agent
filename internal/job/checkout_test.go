package job

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/buildkite/agent/v4/internal/job/githttptest"
	"github.com/buildkite/agent/v4/internal/race"
	"github.com/buildkite/agent/v4/internal/shell"
)

func TestDefaultCheckoutPhase(t *testing.T) {
	ctx := t.Context()

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

func TestPrepareGitSSHKey(t *testing.T) {
	t.Parallel()

	sh, err := shell.New()
	if err != nil {
		t.Fatalf("shell.New() error = %v, want nil", err)
	}

	t.Run("no key configured", func(t *testing.T) {
		executor := &Executor{shell: sh}

		sshKeyPath, cleanup, err := executor.prepareGitSSHKey()
		if err != nil {
			t.Fatalf("executor.prepareGitSSHKey() error = %v, want nil", err)
		}
		if sshKeyPath != "" {
			t.Fatalf("executor.prepareGitSSHKey() path = %q, want empty", sshKeyPath)
		}
		if cleanup != nil {
			t.Fatal("executor.prepareGitSSHKey() cleanup != nil, want nil")
		}
	})

	t.Run("creates key file, augments GIT_SSH_COMMAND, and restores environment", func(t *testing.T) {
		checkoutParent := t.TempDir()
		checkoutPath := filepath.Join(checkoutParent, "checkout")
		sh.Env.Set("BUILDKITE_BUILD_CHECKOUT_PATH", checkoutPath)
		sh.Env.Set("GIT_SSH_COMMAND", "ssh -F ~/.ssh/config")

		executor := &Executor{
			shell: sh,
			ExecutorConfig: ExecutorConfig{
				GitSSHKey:    "super-secret-key",
				PipelineSlug: "test/pipeline",
			},
		}

		sshKeyPath, cleanup, err := executor.prepareGitSSHKey()
		if err != nil {
			t.Fatalf("executor.prepareGitSSHKey() error = %v, want nil", err)
		}
		if cleanup == nil {
			t.Fatal("executor.prepareGitSSHKey() cleanup = nil, want non-nil")
		}
		sshKeyDir := filepath.Dir(sshKeyPath)
		if got, want := filepath.Dir(sshKeyDir), checkoutParent; got != want {
			t.Fatalf("filepath.Dir(sshKeyDir) = %q, want %q", got, want)
		}
		if matched, err := filepath.Match(filepath.Join(checkoutParent, ".buildkite-ssh-key-test-pipeline-*"), sshKeyDir); err != nil || !matched {
			t.Fatalf("sshKeyDir = %q, want buildkite ssh key pattern match (err=%v)", sshKeyDir, err)
		}
		contents, err := os.ReadFile(sshKeyPath)
		if err != nil {
			t.Fatalf("os.ReadFile(%q) error = %v, want nil", sshKeyPath, err)
		}
		if got, want := string(contents), "super-secret-key\n"; got != want {
			t.Fatalf("ssh key contents = %q, want %q", got, want)
		}
		if runtime.GOOS != "windows" {
			info, err := os.Stat(sshKeyPath)
			if err != nil {
				t.Fatalf("os.Stat(%q) error = %v, want nil", sshKeyPath, err)
			}
			if got, want := info.Mode().Perm(), os.FileMode(0o600); got != want {
				t.Fatalf("ssh key permissions = %o, want %o", got, want)
			}
			dirInfo, err := os.Stat(sshKeyDir)
			if err != nil {
				t.Fatalf("os.Stat(%q) error = %v, want nil", sshKeyDir, err)
			}
			if got, want := dirInfo.Mode().Perm(), os.FileMode(0o700); got != want {
				t.Fatalf("ssh key directory permissions = %o, want %o", got, want)
			}
		}
		if got, want := sh.Env.GetString("GIT_SSH_COMMAND", ""), gitSSHCommandForKeyFile(sshKeyPath, "ssh -F ~/.ssh/config"); got != want {
			t.Fatalf("GIT_SSH_COMMAND = %q, want %q", got, want)
		}

		if err := cleanup(); err != nil {
			t.Fatalf("cleanup() error = %v, want nil", err)
		}
		if _, err := os.Stat(sshKeyDir); !os.IsNotExist(err) {
			t.Fatalf("os.Stat(%q) error = %v, want not exist", sshKeyDir, err)
		}
		if got, want := sh.Env.GetString("GIT_SSH_COMMAND", ""), "ssh -F ~/.ssh/config"; got != want {
			t.Fatalf("restored GIT_SSH_COMMAND = %q, want %q", got, want)
		}
	})

	t.Run("creates key file with default ssh command when none exists", func(t *testing.T) {
		checkoutParent := t.TempDir()
		checkoutPath := filepath.Join(checkoutParent, "checkout")
		sh.Env.Set("BUILDKITE_BUILD_CHECKOUT_PATH", checkoutPath)
		sh.Env.Remove("GIT_SSH_COMMAND")

		executor := &Executor{
			shell: sh,
			ExecutorConfig: ExecutorConfig{
				GitSSHKey: "super-secret-key",
			},
		}

		sshKeyPath, cleanup, err := executor.prepareGitSSHKey()
		if err != nil {
			t.Fatalf("executor.prepareGitSSHKey() error = %v, want nil", err)
		}
		if cleanup == nil {
			t.Fatal("executor.prepareGitSSHKey() cleanup = nil, want non-nil")
		}
		if got, want := sh.Env.GetString("GIT_SSH_COMMAND", ""), gitSSHCommandForKeyFile(sshKeyPath, ""); got != want {
			t.Fatalf("GIT_SSH_COMMAND = %q, want %q", got, want)
		}

		if err := cleanup(); err != nil {
			t.Fatalf("cleanup() error = %v, want nil", err)
		}
		if _, exists := sh.Env.Get("GIT_SSH_COMMAND"); exists {
			t.Fatal("GIT_SSH_COMMAND was restored, want unset")
		}
	})
}

func TestDefaultCheckoutPhase_CleansUpGitSSHKeyOnError(t *testing.T) {
	t.Parallel()

	sh, err := shell.New()
	if err != nil {
		t.Fatalf("shell.New() error = %v, want nil", err)
	}

	checkoutParent := t.TempDir()
	checkoutPath := filepath.Join(checkoutParent, "checkout")
	sh.Env.Set("BUILDKITE_BUILD_CHECKOUT_PATH", checkoutPath)

	executor := &Executor{
		shell: sh,
		ExecutorConfig: ExecutorConfig{
			Repository:    filepath.Join(checkoutParent, "does-not-exist.git"),
			Commit:        "HEAD",
			Branch:        "main",
			GitCleanFlags: "-fdq",
			GitSSHKey:     "super-secret-key",
			PipelineSlug:  "test-pipeline",
		},
	}
	t.Cleanup(func() {
		if executor.checkoutRoot != nil {
			_ = executor.checkoutRoot.Close()
			executor.checkoutRoot = nil
		}
	})

	if err := executor.defaultCheckoutPhase(t.Context()); err == nil {
		t.Fatal("executor.defaultCheckoutPhase(t.Context()) error = nil, want non-nil")
	}

	matches, err := filepath.Glob(filepath.Join(checkoutParent, ".buildkite-ssh-key-*"))
	if err != nil {
		t.Fatalf("filepath.Glob() error = %v, want nil", err)
	}
	if len(matches) != 0 {
		t.Fatalf("ssh key files left behind: %v", matches)
	}
}

func TestSkipCheckout(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

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
