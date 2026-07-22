package job

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildkite/agent/v4/internal/shell"
	"github.com/buildkite/bintest/v3"
)

func TestCleanGitSparseCheckoutPaths(t *testing.T) {
	t.Parallel()

	got := cleanGitSparseCheckoutPaths([]string{" .buildkite/ ", "src/", "", " docs "})
	want := []string{".buildkite/", "src/", "docs"}
	if len(got) != len(want) {
		t.Fatalf("cleanGitSparseCheckoutPaths() = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("cleanGitSparseCheckoutPaths() = %#v, want %#v", got, want)
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

	paths := []string{".buildkite/", "src/"}
	git.Expect("sparse-checkout", "set", "--cone", ".buildkite/", "src/").AndExitWith(0)

	active, err := executor.setupSparseCheckout(t.Context(), paths)
	if err != nil {
		t.Fatalf("executor.setupSparseCheckout(ctx, sparsePaths) error = %v, want nil", err)
	}
	if !active {
		t.Fatalf("executor.setupSparseCheckout(ctx, sparsePaths) active = false, want true")
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

	active, err := executor.setupSparseCheckout(t.Context(), nil)
	if err != nil {
		t.Fatalf("executor.setupSparseCheckout(ctx, sparsePaths) error = %v, want nil", err)
	}
	if active {
		t.Fatalf("executor.setupSparseCheckout(ctx, sparsePaths) active = true, want false")
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

	if _, err := executor.setupSparseCheckout(t.Context(), nil); err != nil {
		t.Fatalf("executor.setupSparseCheckout(ctx, sparsePaths) error = %v, want nil", err)
	}

	git.Check(t)
}

func TestSetupSparseCheckout_DisableWithoutPriorSparseConfig(t *testing.T) {
	executor, git, _ := newSparseCheckoutTestExecutor(t)
	defer git.Close() //nolint:errcheck // Best-effort cleanup.

	git.Expect("config").WithAnyArguments().NotCalled()
	git.Expect("sparse-checkout").WithAnyArguments().NotCalled()

	if _, err := executor.setupSparseCheckout(t.Context(), nil); err != nil {
		t.Fatalf("executor.setupSparseCheckout(ctx, sparsePaths) error = %v, want nil", err)
	}

	git.Check(t)
}

func TestResolveSparseCheckout_VersionFallback(t *testing.T) {
	executor, git, out := newSparseCheckoutTestExecutor(t)
	defer git.Close() //nolint:errcheck // Best-effort cleanup.
	executor.GitSparseCheckoutPaths = []string{"src/"}

	git.Expect("--version").AndWriteToStdout("git version 2.25.4").AndExitWith(0)
	git.Expect("sparse-checkout").WithAnyArguments().NotCalled()

	paths := executor.resolveSparseCheckout(t.Context())
	if len(paths) != 0 {
		t.Fatalf("resolveSparseCheckout(ctx) = %#v, want nil (fallback to full checkout)", paths)
	}
	if got, want := out.String(), "Sparse checkout requires git >= 2.27, got 2.25; falling back to full checkout"; !strings.Contains(got, want) {
		t.Fatalf("shell output = %q, want to contain %q", got, want)
	}

	git.Check(t)
}

func TestSetupSparseCheckout_VersionFallbackDisablesPriorSparseConfig(t *testing.T) {
	executor, git, out := newSparseCheckoutTestExecutor(t)
	defer git.Close() //nolint:errcheck // Best-effort cleanup.
	executor.GitSparseCheckoutPaths = []string{"src/"}
	createSparseCheckoutFile(t, executor.shell.Getwd())

	// Old git: resolveSparseCheckout warns and returns nil paths, then
	// setupSparseCheckout disables the prior sparse config left on disk.
	git.Expect("--version").AndWriteToStdout("git version 2.25.4").AndExitWith(0)
	git.Expect("config", "--get", "core.sparseCheckout").AndWriteToStdout("true\n").AndExitWith(0)
	git.Expect("sparse-checkout", "disable").AndExitWith(0)
	git.Expect("config", "--worktree", "--list").AndWriteToStdout("").AndExitWith(0)
	git.Expect("config", "--unset", "extensions.worktreeConfig").AndExitWith(0)

	sparsePaths := executor.resolveSparseCheckout(t.Context())
	if len(sparsePaths) != 0 {
		t.Fatalf("resolveSparseCheckout(ctx) = %#v, want nil (fallback to full checkout)", sparsePaths)
	}

	active, err := executor.setupSparseCheckout(t.Context(), sparsePaths)
	if err != nil {
		t.Fatalf("executor.setupSparseCheckout(ctx, sparsePaths) error = %v, want nil", err)
	}
	if active {
		t.Fatalf("executor.setupSparseCheckout(ctx, sparsePaths) active = true, want false")
	}
	if got, want := out.String(), "Sparse checkout requires git >= 2.27, got 2.25; falling back to full checkout"; !strings.Contains(got, want) {
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
