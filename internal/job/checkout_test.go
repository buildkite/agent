package job

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/buildkite/agent/v3/internal/job/githttptest"
	"github.com/buildkite/agent/v3/internal/race"
	"github.com/buildkite/agent/v3/internal/shell"
	"github.com/buildkite/bintest/v3"
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

func TestParseSparseCheckoutPaths(t *testing.T) {
	t.Parallel()

	got := parseSparseCheckoutPaths(" .buildkite/ ,src/,, docs ")
	want := []string{".buildkite/", "src/", "docs"}
	if len(got) != len(want) {
		t.Fatalf("parseSparseCheckoutPaths() = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("parseSparseCheckoutPaths() = %#v, want %#v", got, want)
		}
	}
}

func TestParseGitVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		output    string
		wantMajor int
		wantMinor int
		wantOK    bool
	}{
		{output: "git version 2.39.5", wantMajor: 2, wantMinor: 39, wantOK: true},
		{output: "git version 2.26.0.windows.1", wantMajor: 2, wantMinor: 26, wantOK: true},
		{output: "not git", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.output, func(t *testing.T) {
			t.Parallel()
			gotMajor, gotMinor, gotOK := parseGitVersion(tt.output)
			if gotMajor != tt.wantMajor || gotMinor != tt.wantMinor || gotOK != tt.wantOK {
				t.Fatalf("parseGitVersion(%q) = (%d, %d, %t), want (%d, %d, %t)", tt.output, gotMajor, gotMinor, gotOK, tt.wantMajor, tt.wantMinor, tt.wantOK)
			}
		})
	}
}

func TestSetupSparseCheckout_Enable(t *testing.T) {
	executor, git, out := newSparseCheckoutTestExecutor(t)
	defer git.Close() //nolint:errcheck // Best-effort cleanup.
	executor.SparseCheckoutPaths = ".buildkite/,src/"

	git.Expect("--version").AndWriteToStdout("git version 2.39.0").AndExitWith(0)
	git.Expect("sparse-checkout", "set", "--cone", ".buildkite/", "src/").AndExitWith(0)

	active, err := executor.setupSparseCheckout(t.Context())
	if err != nil {
		t.Fatalf("executor.setupSparseCheckout(ctx) error = %v, want nil", err)
	}
	if !active {
		t.Fatalf("executor.setupSparseCheckout(ctx) active = false, want true")
	}
	if got, want := out.String(), "Setting up sparse checkout for paths: .buildkite/,src/"; !strings.Contains(got, want) {
		t.Fatalf("shell output = %q, want to contain %q", got, want)
	}

	git.Check(t)
}

func TestSetupSparseCheckout_DisableWithPriorSparseConfig(t *testing.T) {
	executor, git, out := newSparseCheckoutTestExecutor(t)
	defer git.Close() //nolint:errcheck // Best-effort cleanup.
	createSparseCheckoutFile(t, executor.shell.Getwd())

	git.Expect("config", "--get", "core.sparseCheckout").AndWriteToStdout("true\n").AndExitWith(0)
	git.Expect("sparse-checkout", "disable").AndExitWith(0)
	git.Expect("config", "--worktree", "--list").AndWriteToStdout("").AndExitWith(0)
	git.Expect("config", "--unset", "extensions.worktreeConfig").AndExitWith(0)

	active, err := executor.setupSparseCheckout(t.Context())
	if err != nil {
		t.Fatalf("executor.setupSparseCheckout(ctx) error = %v, want nil", err)
	}
	if active {
		t.Fatalf("executor.setupSparseCheckout(ctx) active = true, want false")
	}
	if got, want := out.String(), "Disabling sparse checkout from previous build"; !strings.Contains(got, want) {
		t.Fatalf("shell output = %q, want to contain %q", got, want)
	}

	git.Check(t)
}

func TestSetupSparseCheckout_DisablePreservesOtherWorktreeConfig(t *testing.T) {
	executor, git, _ := newSparseCheckoutTestExecutor(t)
	defer git.Close() //nolint:errcheck // Best-effort cleanup.
	createSparseCheckoutFile(t, executor.shell.Getwd())

	git.Expect("config", "--get", "core.sparseCheckout").AndWriteToStdout("true\n").AndExitWith(0)
	git.Expect("sparse-checkout", "disable").AndExitWith(0)
	git.Expect("config", "--worktree", "--list").AndWriteToStdout("user.something=value\n").AndExitWith(0)
	git.Expect("config", "--unset", "extensions.worktreeConfig").NotCalled()

	if _, err := executor.setupSparseCheckout(t.Context()); err != nil {
		t.Fatalf("executor.setupSparseCheckout(ctx) error = %v, want nil", err)
	}

	git.Check(t)
}

func TestSetupSparseCheckout_DisableWithoutPriorSparseConfig(t *testing.T) {
	executor, git, _ := newSparseCheckoutTestExecutor(t)
	defer git.Close() //nolint:errcheck // Best-effort cleanup.

	git.Expect("config").WithAnyArguments().NotCalled()
	git.Expect("sparse-checkout").WithAnyArguments().NotCalled()

	if _, err := executor.setupSparseCheckout(t.Context()); err != nil {
		t.Fatalf("executor.setupSparseCheckout(ctx) error = %v, want nil", err)
	}

	git.Check(t)
}

func TestSetupSparseCheckout_VersionFallback(t *testing.T) {
	executor, git, out := newSparseCheckoutTestExecutor(t)
	defer git.Close() //nolint:errcheck // Best-effort cleanup.
	executor.SparseCheckoutPaths = "src/"

	git.Expect("--version").AndWriteToStdout("git version 2.25.4").AndExitWith(0)
	git.Expect("sparse-checkout").WithAnyArguments().NotCalled()

	active, err := executor.setupSparseCheckout(t.Context())
	if err != nil {
		t.Fatalf("executor.setupSparseCheckout(ctx) error = %v, want nil", err)
	}
	if active {
		t.Fatalf("executor.setupSparseCheckout(ctx) active = true, want false")
	}
	if got, want := out.String(), "Sparse checkout requires git >= 2.26; falling back to full checkout"; !strings.Contains(got, want) {
		t.Fatalf("shell output = %q, want to contain %q", got, want)
	}

	git.Check(t)
}

func TestSetupSparseCheckout_VersionFallbackDisablesPriorSparseConfig(t *testing.T) {
	executor, git, out := newSparseCheckoutTestExecutor(t)
	defer git.Close() //nolint:errcheck // Best-effort cleanup.
	executor.SparseCheckoutPaths = "src/"
	createSparseCheckoutFile(t, executor.shell.Getwd())

	git.Expect("--version").AndWriteToStdout("git version 2.25.4").AndExitWith(0)
	git.Expect("config", "--get", "core.sparseCheckout").AndWriteToStdout("true\n").AndExitWith(0)
	git.Expect("sparse-checkout", "disable").AndExitWith(0)
	git.Expect("config", "--worktree", "--list").AndWriteToStdout("").AndExitWith(0)
	git.Expect("config", "--unset", "extensions.worktreeConfig").AndExitWith(0)

	if _, err := executor.setupSparseCheckout(t.Context()); err != nil {
		t.Fatalf("executor.setupSparseCheckout(ctx) error = %v, want nil", err)
	}
	if got, want := out.String(), "Sparse checkout requires git >= 2.26; falling back to full checkout"; !strings.Contains(got, want) {
		t.Fatalf("shell output = %q, want to contain %q", got, want)
	}
	if got, want := out.String(), "Disabling sparse checkout from previous build"; !strings.Contains(got, want) {
		t.Fatalf("shell output = %q, want to contain %q", got, want)
	}

	git.Check(t)
}

func newSparseCheckoutTestExecutor(t *testing.T) (*Executor, *bintest.Mock, *bytes.Buffer) {
	t.Helper()

	pathDir := t.TempDir()
	git, err := bintest.NewMock(filepath.Join(pathDir, "git"))
	if err != nil {
		t.Fatalf("bintest.NewMock(git) error = %v, want nil", err)
	}

	t.Setenv("PATH", pathDir)

	out := new(bytes.Buffer)
	sh := shell.NewTestShell(t,
		shell.WithLogger(shell.NewWriterLogger(out, false, nil)),
		shell.WithStdout(out),
		shell.WithWD(t.TempDir()),
	)
	sh.Env.Set("PATH", pathDir)

	return &Executor{shell: sh}, git, out
}

func createSparseCheckoutFile(t *testing.T, checkoutDir string) {
	t.Helper()

	sparseCheckoutPath := filepath.Join(checkoutDir, ".git", "info", "sparse-checkout")
	if err := os.MkdirAll(filepath.Dir(sparseCheckoutPath), 0o755); err != nil {
		t.Fatalf("os.MkdirAll(%q) error = %v, want nil", filepath.Dir(sparseCheckoutPath), err)
	}
	if err := os.WriteFile(sparseCheckoutPath, []byte("/*\n"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v, want nil", sparseCheckoutPath, err)
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
