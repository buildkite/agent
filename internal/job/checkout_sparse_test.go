package job

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/internal/shell"
	"github.com/buildkite/bintest/v3"
	"github.com/google/go-cmp/cmp"
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

func TestParseSparseCheckoutMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mode    string
		want    SparseCheckoutMode
		wantErr bool
	}{
		{name: "empty_defaults_to_cone", want: SparseCheckoutModeCone},
		{name: "cone", mode: "cone", want: SparseCheckoutModeCone},
		{name: "no_cone", mode: "no-cone", want: SparseCheckoutModeNoCone},
		{name: "wrong_case", mode: "Cone", wantErr: true},
		{name: "missing_hyphen", mode: "nocone", wantErr: true},
		{name: "flag_spelling", mode: "--no-cone", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseSparseCheckoutMode(tt.mode)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseSparseCheckoutMode(%q) error = nil, want non-nil", tt.mode)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseSparseCheckoutMode(%q) error = %v, want nil", tt.mode, err)
			}
			if got != tt.want {
				t.Errorf("ParseSparseCheckoutMode(%q) = %q, want %q", tt.mode, got, tt.want)
			}
		})
	}
}

func TestSetupSparseCheckout_Enable(t *testing.T) {
	executor, git, out := newSparseCheckoutTestExecutor(t)
	defer git.Close() //nolint:errcheck // Best-effort cleanup.

	sc := sparseCheckout{paths: []string{".buildkite/", "src/"}, mode: SparseCheckoutModeCone, modeEnforced: true}
	git.Expect("sparse-checkout", "set", "--cone", "--", ".buildkite/", "src/").AndExitWith(0)

	active, err := executor.setupSparseCheckout(t.Context(), sc)
	if err != nil {
		t.Fatalf("executor.setupSparseCheckout(ctx, sc) error = %v, want nil", err)
	}
	if !active {
		t.Fatalf("executor.setupSparseCheckout(ctx, sc) active = false, want true")
	}
	if got, want := out.String(), "Setting up sparse checkout (cone mode) for paths: .buildkite/,src/"; !strings.Contains(got, want) {
		t.Fatalf("shell output = %q, want to contain %q", got, want)
	}

	git.Check(t)
}

func TestSetupSparseCheckout_NoCone(t *testing.T) {
	executor, git, out := newSparseCheckoutTestExecutor(t)
	defer git.Close() //nolint:errcheck // Best-effort cleanup.

	sc := sparseCheckout{paths: []string{"/*", "!/docs/"}, mode: SparseCheckoutModeNoCone, modeEnforced: true}
	git.Expect("sparse-checkout", "set", "--no-cone", "--", "/*", "!/docs/").AndExitWith(0)

	active, err := executor.setupSparseCheckout(t.Context(), sc)
	if err != nil {
		t.Fatalf("executor.setupSparseCheckout(ctx, sc) error = %v, want nil", err)
	}
	if !active {
		t.Fatalf("executor.setupSparseCheckout(ctx, sc) active = false, want true")
	}
	if got, want := out.String(), "Setting up sparse checkout (no-cone mode) for paths: /*,!/docs/"; !strings.Contains(got, want) {
		t.Fatalf("shell output = %q, want to contain %q", got, want)
	}

	git.Check(t)
}

// TestSetupSparseCheckout_UnenforcedMode covers git 2.27–2.34, where
// `sparse-checkout set` has no --cone option and silently treats an unrecognised
// one as a pattern. The flag must be omitted so it can't land in the
// sparse-checkout file, while `--` still guards the paths. Since git will apply
// whatever mode the repository config says, the build log has to say so rather
// than claiming the requested mode is in force.
func TestSetupSparseCheckout_UnenforcedMode(t *testing.T) {
	executor, git, out := newSparseCheckoutTestExecutor(t)
	defer git.Close() //nolint:errcheck // Best-effort cleanup.

	sc := sparseCheckout{paths: []string{"src/"}, mode: SparseCheckoutModeCone}
	git.Expect("sparse-checkout", "set", "--", "src/").AndExitWith(0)

	if _, err := executor.setupSparseCheckout(t.Context(), sc); err != nil {
		t.Fatalf("executor.setupSparseCheckout(ctx, sc) error = %v, want nil", err)
	}
	if got, want := out.String(), "older than 2.35 and can't be told which sparse checkout mode to use"; !strings.Contains(got, want) {
		t.Errorf("shell output = %q, want to contain %q", got, want)
	}

	git.Check(t)
}

func TestResolveSparseCheckout_Modes(t *testing.T) {
	tests := []struct {
		name             string
		mode             string
		gitVersion       string
		gitVersionExit   int
		wantActive       bool
		wantMode         SparseCheckoutMode
		wantModeEnforced bool
		wantWarning      string
	}{
		{
			name:             "cone_on_modern_git",
			gitVersion:       "git version 2.43.0",
			wantActive:       true,
			wantMode:         SparseCheckoutModeCone,
			wantModeEnforced: true,
		},
		{
			// Cone mode is available as soon as `sparse-checkout set` is; below
			// 2.35 git just takes the interpretation from the repository config
			// rather than from the flag.
			name:       "cone_below_mode_flag_support",
			gitVersion: "git version 2.30.2",
			wantActive: true,
			wantMode:   SparseCheckoutModeCone,
		},
		{
			name:             "no_cone_on_modern_git",
			mode:             "no-cone",
			gitVersion:       "git version 2.35.0",
			wantActive:       true,
			wantMode:         SparseCheckoutModeNoCone,
			wantModeEnforced: true,
		},
		{
			// `set` gained --no-cone in 2.35; older git would write the flag into
			// the sparse-checkout file as a pattern and leave the mode alone.
			name:        "no_cone_below_mode_flag_support_falls_back",
			mode:        "no-cone",
			gitVersion:  "git version 2.34.1",
			wantWarning: "Sparse checkout in no-cone mode requires git >= 2.35, got 2.34; falling back to full checkout",
		},
		{
			name:        "cone_below_minimum_falls_back",
			gitVersion:  "git version 2.25.4",
			wantWarning: "Sparse checkout requires git >= 2.27, got 2.25; falling back to full checkout",
		},
		{
			// Unreadable version output is not a reason to fail the build, but it
			// is a reason not to guess about sparse support.
			name:        "unparseable_version_falls_back",
			gitVersion:  "git version banana",
			wantWarning: `Sparse checkout requires git >= 2.27; falling back to full checkout (parsing git version from "git version banana")`,
		},
		{
			name:           "failing_version_command_falls_back",
			gitVersion:     "",
			gitVersionExit: 1,
			wantWarning:    "Sparse checkout requires git >= 2.27; falling back to full checkout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executor, git, out := newSparseCheckoutTestExecutor(t)
			defer git.Close() //nolint:errcheck // Best-effort cleanup.
			executor.GitSparseCheckoutPaths = []string{"src/"}
			executor.GitSparseCheckoutMode = tt.mode

			git.Expect("--version").AndWriteToStdout(tt.gitVersion).AndExitWith(tt.gitVersionExit)
			git.Expect("sparse-checkout").WithAnyArguments().NotCalled()

			sc, err := executor.resolveSparseCheckout(t.Context())
			if err != nil {
				t.Fatalf("executor.resolveSparseCheckout(ctx) error = %v, want nil", err)
			}
			if got := sc.active(); got != tt.wantActive {
				t.Errorf("sparseCheckout.active() = %t, want %t", got, tt.wantActive)
			}
			if got := sc.mode; got != tt.wantMode {
				t.Errorf("sparseCheckout.mode = %q, want %q", got, tt.wantMode)
			}
			if got := sc.modeEnforced; got != tt.wantModeEnforced {
				t.Errorf("sparseCheckout.modeEnforced = %t, want %t", got, tt.wantModeEnforced)
			}
			switch {
			case tt.wantWarning != "":
				if !strings.Contains(out.String(), tt.wantWarning) {
					t.Errorf("shell output = %q, want to contain %q", out.String(), tt.wantWarning)
				}
			default:
				// A supported git must resolve quietly: a stray warning here would
				// tell every sparse build that something is wrong when it isn't.
				if strings.Contains(out.String(), "falling back to full checkout") {
					t.Errorf("shell output = %q, want no fallback warning", out.String())
				}
			}

			git.Check(t)
		})
	}
}

func TestSparseCheckoutLFSInclude(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		sc   sparseCheckout
		want []string
	}{
		{
			name: "cone_paths_scope_lfs",
			sc:   sparseCheckout{paths: []string{"src/"}, mode: SparseCheckoutModeCone, modeEnforced: true},
			want: []string{"src/"},
		},
		{
			// `git lfs fetch --include` has no negation and `git lfs checkout` takes
			// pathspecs, so non-cone patterns must not be reused for LFS.
			name: "no_cone_patterns_do_not_scope_lfs",
			sc:   sparseCheckout{paths: []string{"/*", "!/docs/"}, mode: SparseCheckoutModeNoCone, modeEnforced: true},
		},
		{
			name: "inactive_does_not_scope_lfs",
			sc:   sparseCheckout{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if diff := cmp.Diff(tt.sc.lfsInclude(), tt.want); diff != "" {
				t.Errorf("sparseCheckout.lfsInclude() diff (-got +want):\n%s", diff)
			}
		})
	}
}

func TestResolveSparseCheckout_InvalidMode(t *testing.T) {
	executor, git, _ := newSparseCheckoutTestExecutor(t)
	defer git.Close() //nolint:errcheck // Best-effort cleanup.
	executor.GitSparseCheckoutPaths = []string{"src/"}
	executor.GitSparseCheckoutMode = "conical"

	// An invalid mode must fail before any git command runs, rather than
	// silently checking out the wrong files.
	git.Expect("--version").WithAnyArguments().NotCalled()
	git.Expect("sparse-checkout").WithAnyArguments().NotCalled()

	if _, err := executor.resolveSparseCheckout(t.Context()); err == nil {
		t.Fatalf("executor.resolveSparseCheckout(ctx) error = nil, want non-nil")
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

	active, err := executor.setupSparseCheckout(t.Context(), sparseCheckout{})
	if err != nil {
		t.Fatalf("executor.setupSparseCheckout(ctx, sc) error = %v, want nil", err)
	}
	if active {
		t.Fatalf("executor.setupSparseCheckout(ctx, sc) active = true, want false")
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

	if _, err := executor.setupSparseCheckout(t.Context(), sparseCheckout{}); err != nil {
		t.Fatalf("executor.setupSparseCheckout(ctx, sc) error = %v, want nil", err)
	}

	git.Check(t)
}

func TestSetupSparseCheckout_DisableWithoutPriorSparseConfig(t *testing.T) {
	executor, git, _ := newSparseCheckoutTestExecutor(t)
	defer git.Close() //nolint:errcheck // Best-effort cleanup.

	git.Expect("config").WithAnyArguments().NotCalled()
	git.Expect("sparse-checkout").WithAnyArguments().NotCalled()

	if _, err := executor.setupSparseCheckout(t.Context(), sparseCheckout{}); err != nil {
		t.Fatalf("executor.setupSparseCheckout(ctx, sc) error = %v, want nil", err)
	}

	git.Check(t)
}

func TestSetupSparseCheckout_VersionFallbackDisablesPriorSparseConfig(t *testing.T) {
	executor, git, out := newSparseCheckoutTestExecutor(t)
	defer git.Close() //nolint:errcheck // Best-effort cleanup.
	executor.GitSparseCheckoutPaths = []string{"src/"}
	createSparseCheckoutFile(t, executor.shell.Getwd())

	// Old git: resolveSparseCheckout warns and resolves to a full checkout, then
	// setupSparseCheckout disables the prior sparse config left on disk.
	git.Expect("--version").AndWriteToStdout("git version 2.25.4").AndExitWith(0)
	git.Expect("config", "--get", "core.sparseCheckout").AndWriteToStdout("true\n").AndExitWith(0)
	git.Expect("sparse-checkout", "disable").AndExitWith(0)
	git.Expect("config", "--worktree", "--list").AndWriteToStdout("").AndExitWith(0)
	git.Expect("config", "--unset", "extensions.worktreeConfig").AndExitWith(0)

	sc, err := executor.resolveSparseCheckout(t.Context())
	if err != nil {
		t.Fatalf("executor.resolveSparseCheckout(ctx) error = %v, want nil", err)
	}
	if sc.active() {
		t.Fatalf("sparseCheckout.active() = true, want false (fallback to full checkout)")
	}

	active, err := executor.setupSparseCheckout(t.Context(), sc)
	if err != nil {
		t.Fatalf("executor.setupSparseCheckout(ctx, sc) error = %v, want nil", err)
	}
	if active {
		t.Fatalf("executor.setupSparseCheckout(ctx, sc) active = true, want false")
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
