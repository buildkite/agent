package job

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/buildkite/agent/v3/internal/job/githttptest"
	"github.com/buildkite/agent/v3/internal/race"
	"github.com/buildkite/agent/v3/internal/shell"
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

func TestGitLFSBinaryMissing(t *testing.T) {
	// Not parallel: the test manipulates PATH via t.Setenv, which modifies
	// process-global state.

	ctx := t.Context()

	sh, err := shell.New()
	if err != nil {
		t.Fatalf("shell.New() error = %v, want nil", err)
	}

	// Use an empty PATH to simulate git-lfs not being installed
	sh.Env.Set("PATH", "")

	executor := &Executor{
		shell: sh,
		ExecutorConfig: ExecutorConfig{
			Repository:    "https://github.com/buildkite/agent.git",
			GitLFSEnabled: true,
		},
	}

	err = executor.checkout(ctx)
	if err == nil {
		t.Fatalf("executor.checkout(ctx) error = nil, want error containing %q", "git-lfs binary is not found on PATH")
	}
	if !strings.Contains(err.Error(), "git-lfs binary is not found on PATH") {
		t.Errorf("executor.checkout(ctx) error = %q, want it to contain %q", err.Error(), "git-lfs binary is not found on PATH")
	}
}

func TestDefaultCheckoutPhase_GitLFS(t *testing.T) {
	// Not parallel: subtests manipulate PATH via t.Setenv, which modifies
	// process-global state.
	ctx := t.Context()

	t.Setenv("GIT_AUTHOR_NAME", "Buildkite Agent")
	t.Setenv("GIT_AUTHOR_EMAIL", "agent@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Buildkite Agent")
	t.Setenv("GIT_COMMITTER_EMAIL", "agent@example.com")

	// gitOnlyBinDir returns a temp dir containing git (via a symlink on Unix or
	// a .bat wrapper on Windows) but no git-lfs, so exec.LookPath("git-lfs")
	// will fail while git commands still work.
	gitOnlyBinDir := func(t *testing.T) string {
		t.Helper()
		gitBin, err := exec.LookPath("git")
		if err != nil {
			t.Fatalf("exec.LookPath(\"git\") error = %v", err)
		}
		binDir := t.TempDir()
		if runtime.GOOS == "windows" {
			// Use a .bat wrapper to avoid copying the multi-MB binary and to
			// sidestep the symlink-privilege requirement on Windows.
			wrapper := fmt.Sprintf("@echo off\r\n\"%s\" %%*\r\n", gitBin)
			if err := os.WriteFile(filepath.Join(binDir, "git.bat"), []byte(wrapper), 0o755); err != nil {
				t.Fatalf("os.WriteFile() error = %v", err)
			}
		} else {
			if err := os.Symlink(gitBin, filepath.Join(binDir, "git")); err != nil {
				t.Fatalf("os.Symlink() error = %v", err)
			}
		}
		return binDir
	}

	// fakeLFSBinDir returns a temp dir that has git (via gitOnlyBinDir) plus a
	// fake git-lfs whose behaviour is defined by the provided scripts.
	// unixScript is a #!/bin/sh script; winBatch is a .bat file body.
	fakeLFSBinDir := func(t *testing.T, unixScript, winBatch string) string {
		t.Helper()
		binDir := gitOnlyBinDir(t)
		if runtime.GOOS == "windows" {
			if err := os.WriteFile(filepath.Join(binDir, "git-lfs.bat"), []byte(winBatch), 0o755); err != nil {
				t.Fatalf("os.WriteFile() error = %v", err)
			}
		} else {
			if err := os.WriteFile(filepath.Join(binDir, "git-lfs"), []byte(unixScript), 0o755); err != nil {
				t.Fatalf("os.WriteFile() error = %v", err)
			}
		}
		return binDir
	}

	tests := []struct {
		name       string
		lfsEnabled bool
		setupPath  func(t *testing.T)
		wantErr    string
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
			name:       "LFS enabled git lfs command fails",
			lfsEnabled: true,
			setupPath: func(t *testing.T) {
				// Git for Windows ships its own git-lfs.exe inside
				// GIT_EXEC_PATH, which git resolves before falling back to
				// PATH. We can't fool git's subcommand lookup with a PATH
				// override the way we can fool Go's exec.LookPath.
				if runtime.GOOS == "windows" {
					t.Skip("Not runnable on Windows: git for Windows uses bundled git-lfs.exe regardless of PATH")
				}
				t.Setenv("PATH", fakeLFSBinDir(t,
					"#!/bin/sh\nexit 1\n",
					"@echo off\r\nexit /b 1\r\n",
				))
			},
			wantErr: "installing git lfs filter",
		},
		{
			name:       "LFS enabled git lfs fetch fails",
			lfsEnabled: true,
			setupPath: func(t *testing.T) {
				if runtime.GOOS == "windows" {
					t.Skip("Not runnable on Windows: git for Windows uses bundled git-lfs.exe regardless of PATH")
				}
				t.Setenv("PATH", fakeLFSBinDir(t,
					"#!/bin/sh\ncase \"$1\" in\n  install) exit 0 ;;\n  *) exit 1 ;;\nesac\n",
					"@echo off\r\nif \"%1\"==\"install\" exit /b 0\r\nexit /b 1\r\n",
				))
			},
			wantErr: "git lfs fetch",
		},
	}

	s := githttptest.NewServer()
	t.Cleanup(s.Close)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up the remote repository BEFORE restricting PATH so that
			// githttptest's git operations use the real git binary.
			projectName := "test-" + strings.ReplaceAll(strings.ToLower(tt.name), " ", "-")
			if err := s.CreateRepository(projectName); err != nil {
				t.Fatalf("s.CreateRepository(%q) error = %v", projectName, err)
			}
			out, err := s.InitRepository(projectName)
			if err != nil {
				t.Fatalf("s.InitRepository(%q) error = %v, output: %s", projectName, err, out)
			}

			// Restrict PATH after the repo is initialised.
			if tt.setupPath != nil {
				tt.setupPath(t)
			}

			sh, err := shell.New()
			if err != nil {
				t.Fatalf("shell.New() error = %v", err)
			}

			// Use os.MkdirTemp + best-effort cleanup rather than t.TempDir():
			// on Windows, git's child processes (credential helpers, git-lfs
			// filter-process) can hold file handles open past their parent's
			// exit, and t.TempDir()'s strict cleanup fails the test.
			checkoutDir, err := os.MkdirTemp("", "checkout-path-")
			if err != nil {
				t.Fatalf("os.MkdirTemp() error = %v", err)
			}
			t.Cleanup(func() {
				os.RemoveAll(checkoutDir) //nolint:errcheck // Best-effort cleanup.
			})
			buildDir, err := os.MkdirTemp("", "build-path-")
			if err != nil {
				t.Fatalf("os.MkdirTemp() error = %v", err)
			}
			t.Cleanup(func() {
				os.RemoveAll(buildDir) //nolint:errcheck // Best-effort cleanup.
			})
			sh.Env.Set("BUILDKITE_BUILD_CHECKOUT_PATH", checkoutDir)

			executor := &Executor{
				shell: sh,
				ExecutorConfig: ExecutorConfig{
					Commit:        "HEAD",
					Branch:        "main",
					GitCleanFlags: "-f -d -x",
					BuildPath:     buildDir,
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
