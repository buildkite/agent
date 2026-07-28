package job

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/buildkite/agent/v3/internal/osutil"
	"github.com/buildkite/agent/v3/internal/shell"
)

// Sparse checkout modes, selected by BUILDKITE_GIT_SPARSE_CHECKOUT_MODE, control
// how the configured paths are interpreted by git.
const (
	// SparseCheckoutModeCone treats each path as a directory to include, along
	// with files in its ancestors. This is git's own default and only accepts
	// directory names, not patterns.
	SparseCheckoutModeCone = "cone"

	// SparseCheckoutModeNoCone treats each path as a gitignore-style pattern,
	// which allows exclusions ("!/docs/") and globs at the cost of the
	// performance benefits of cone mode.
	SparseCheckoutModeNoCone = "no-cone"
)

// SparseCheckoutModes lists the accepted mode values.
var SparseCheckoutModes = []string{SparseCheckoutModeCone, SparseCheckoutModeNoCone}

// ParseSparseCheckoutMode validates a sparse-checkout mode value. An empty
// string selects the default (cone).
func ParseSparseCheckoutMode(mode string) (string, error) {
	if mode == "" {
		return SparseCheckoutModeCone, nil
	}
	if !slices.Contains(SparseCheckoutModes, mode) {
		return "", fmt.Errorf("invalid sparse checkout mode %q, must be one of %v", mode, SparseCheckoutModes)
	}
	return mode, nil
}

// sparseCheckout is the sparse-checkout configuration resolved for this build.
// The zero value means check out the full tree.
type sparseCheckout struct {
	paths []string
	mode  string

	// pinMode reports whether git accepts the --cone/--no-cone options on
	// `sparse-checkout set` (git >= 2.35). Older git parses an unrecognised
	// option as just another pattern rather than rejecting it, so the flag has
	// to be omitted there and the mode inherited from the repository config.
	pinMode bool
}

func (s sparseCheckout) active() bool { return len(s.paths) > 0 }

func (s sparseCheckout) noCone() bool { return s.mode == SparseCheckoutModeNoCone }

// lfsInclude returns the paths to scope Git LFS to, or nil to fetch all LFS
// objects. Only cone-mode paths can be reused for LFS: `git lfs fetch --include`
// has no negation (that's --exclude), and `git lfs checkout` takes pathspecs, so
// a non-cone pattern such as "!/docs/" would silently match nothing and leave
// LFS files inside the sparse set as pointer files.
func (s sparseCheckout) lfsInclude() []string {
	if !s.active() || s.noCone() {
		return nil
	}
	return s.paths
}

// resolveSparseCheckout returns the sparse-checkout configuration for this
// build. It resolves to a full checkout when no paths were requested, or when
// git is too old to honour the requested mode.
func (e *Executor) resolveSparseCheckout(ctx context.Context) (sparseCheckout, error) {
	paths := cleanGitSparseCheckoutPaths(e.GitSparseCheckoutPaths)
	if len(paths) == 0 {
		return sparseCheckout{}, nil
	}

	mode, err := ParseSparseCheckoutMode(e.GitSparseCheckoutMode)
	if err != nil {
		return sparseCheckout{}, err
	}

	// `git sparse-checkout set` was promoted from experimental to stable in git
	// 2.27, but it only learned --cone/--no-cone in 2.35, and older git parses an
	// unrecognised option as another pattern instead of rejecting it. Non-cone
	// mode therefore can't be requested at all below 2.35, so fall back to a full
	// checkout rather than silently checking out the wrong files. Cone mode is
	// still accepted from 2.27, where the interpretation comes from the
	// repository's core.sparseCheckoutCone rather than from the flag.
	minMinor := 27
	requirement := "Sparse checkout requires git >= 2.27"
	if mode == SparseCheckoutModeNoCone {
		minMinor = 35
		requirement = "Sparse checkout in no-cone mode requires git >= 2.35"
	}

	major, minor, err := gitVersion(ctx, e.shell)
	if err != nil {
		e.shell.Warningf("%s; falling back to full checkout (%v)", requirement, err)
		return sparseCheckout{}, nil
	}
	if !versionAtLeast(major, minor, 2, minMinor) {
		e.shell.Warningf("%s, got %d.%d; falling back to full checkout", requirement, major, minor)
		return sparseCheckout{}, nil
	}

	return sparseCheckout{
		paths:   paths,
		mode:    mode,
		pinMode: versionAtLeast(major, minor, 2, 35),
	}, nil
}

func cleanGitSparseCheckoutPaths(paths []string) []string {
	cleaned := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path != "" {
			cleaned = append(cleaned, path)
		}
	}
	return cleaned
}

// gitVersion returns the major and minor version of the local git binary.
func gitVersion(ctx context.Context, sh *shell.Shell) (major, minor int, err error) {
	output, err := sh.Command("git", "--version").RunAndCaptureStdout(ctx)
	if err != nil {
		return 0, 0, err
	}

	major, minor, ok := parseGitVersion(strings.TrimSpace(output))
	if !ok {
		return 0, 0, fmt.Errorf("parsing git version from %q", strings.TrimSpace(output))
	}
	return major, minor, nil
}

// versionAtLeast reports whether major.minor is at least reqMajor.reqMinor.
func versionAtLeast(major, minor, reqMajor, reqMinor int) bool {
	if major != reqMajor {
		return major > reqMajor
	}
	return minor >= reqMinor
}

func parseGitVersion(output string) (major, minor int, ok bool) {
	if _, err := fmt.Sscanf(output, "git version %d.%d", &major, &minor); err != nil {
		return 0, 0, false
	}
	return major, minor, true
}

// setupSparseCheckout configures git sparse checkout for the resolved paths.
// When sc is inactive it does a full checkout instead, disabling any prior
// sparse checkout configuration. It returns true when sparse checkout is
// applied, so callers can skip steps that need the full tree (e.g. submodule
// init).
func (e *Executor) setupSparseCheckout(ctx context.Context, sc sparseCheckout) (bool, error) {
	if !sc.active() {
		e.disableSparseCheckoutIfConfigured(ctx)
		return false, nil
	}

	e.shell.Commentf("Setting up sparse checkout (%s mode) for paths: %s", sc.mode, strings.Join(sc.paths, ","))

	args := []string{"sparse-checkout", "set"}
	if sc.pinMode {
		args = append(args, "--"+sc.mode)
	}
	// `--` keeps git from parsing a path as an option: paths can come from the
	// pipeline, and git quietly accepts an unrecognised option as a pattern.
	args = append(args, "--")
	args = append(args, sc.paths...)

	if err := e.shell.Command("git", args...).Run(ctx); err != nil {
		return false, fmt.Errorf("setting sparse checkout paths: %w", err)
	}

	return true, nil
}

func (e *Executor) disableSparseCheckoutIfConfigured(ctx context.Context) {
	if !sparseCheckoutMayBeConfigured(e.shell) {
		return
	}

	sparseOutput, err := e.shell.Command("git", "config", "--get", "core.sparseCheckout").RunAndCaptureStdout(ctx, shell.ShowStderr(false))
	if err != nil || strings.TrimSpace(sparseOutput) != "true" {
		return
	}

	e.shell.Commentf("Disabling sparse checkout from previous build")
	if err := e.shell.Command("git", "sparse-checkout", "disable").Run(ctx); err != nil {
		e.shell.Warningf("Failed to disable sparse checkout: %v", err)
	}

	// `sparse-checkout disable` leaves extensions.worktreeConfig set, which
	// can cause problems for subsequent git operations. Only unset it if no
	// other worktree-scoped config remains, to avoid clobbering user config.
	worktreeConfig, err := e.shell.Command("git", "config", "--worktree", "--list").RunAndCaptureStdout(ctx, shell.ShowStderr(false))
	if err == nil && strings.TrimSpace(worktreeConfig) == "" {
		_ = e.shell.Command("git", "config", "--unset", "extensions.worktreeConfig").Run(ctx)
	}
}

// sparseCheckoutMayBeConfigured does a cheap filesystem check for marker files
// that indicate sparse checkout (or the worktree-config extension that
// `sparse-checkout` enables) might already be in effect, so we can avoid
// shelling out to `git config` on every checkout. It resolves the .git dir
// directly to handle the worktree/submodule case where .git is a file
// containing `gitdir: <path>`.
func sparseCheckoutMayBeConfigured(sh *shell.Shell) bool {
	gitDir := filepath.Join(sh.Getwd(), ".git")
	if data, err := os.ReadFile(gitDir); err == nil && bytes.HasPrefix(data, []byte("gitdir:")) {
		gitDirValue := strings.TrimSpace(string(bytes.TrimPrefix(data, []byte("gitdir:"))))
		if !filepath.IsAbs(gitDirValue) {
			gitDirValue = filepath.Join(sh.Getwd(), gitDirValue)
		}
		gitDir = gitDirValue
	}

	return osutil.FileExists(filepath.Join(gitDir, "info", "sparse-checkout")) ||
		osutil.FileExists(filepath.Join(gitDir, "config.worktree"))
}
