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

// SparseCheckoutMode selects how git interprets the configured sparse-checkout
// paths. It is set by BUILDKITE_GIT_SPARSE_CHECKOUT_MODE.
type SparseCheckoutMode string

const (
	// SparseCheckoutModeCone treats each path as a directory to include, along
	// with files in its ancestors. This is git's own default and only accepts
	// directory names, not patterns.
	SparseCheckoutModeCone SparseCheckoutMode = "cone"

	// SparseCheckoutModeNoCone treats each path as a gitignore-style pattern,
	// which allows exclusions ("!/docs/") and globs at the cost of the
	// performance benefits of cone mode.
	SparseCheckoutModeNoCone SparseCheckoutMode = "no-cone"
)

// SparseCheckoutModes lists the accepted mode values, default first.
var SparseCheckoutModes = []SparseCheckoutMode{SparseCheckoutModeCone, SparseCheckoutModeNoCone}

func (m SparseCheckoutMode) String() string { return string(m) }

// ParseSparseCheckoutMode returns the canonical mode for a configured value. An
// empty string selects the default (cone).
func ParseSparseCheckoutMode(mode string) (SparseCheckoutMode, error) {
	if mode == "" {
		return SparseCheckoutModeCone, nil
	}
	if m := SparseCheckoutMode(mode); slices.Contains(SparseCheckoutModes, m) {
		return m, nil
	}
	return "", fmt.Errorf("invalid sparse checkout mode %q, must be one of %v", mode, SparseCheckoutModes)
}

// git version floors for sparse checkout.
var (
	// minGitSparseCheckout is the first version with a stable
	// `git sparse-checkout set`.
	minGitSparseCheckout = gitVer{2, 27}

	// minGitSparseCheckoutMode is the first version whose `sparse-checkout set`
	// accepts --cone/--no-cone. Below it, git parses an unrecognised option as
	// just another pattern rather than rejecting it, so the mode can only be
	// requested from 2.35 on.
	minGitSparseCheckoutMode = gitVer{2, 35}
)

// sparseCheckout is the sparse-checkout configuration resolved for this build.
// The zero value means check out the full tree.
type sparseCheckout struct {
	paths []string
	mode  SparseCheckoutMode

	// modeEnforced reports whether mode is what git will actually apply. Below
	// minGitSparseCheckoutMode the mode option can't be passed, so git falls
	// back to the repository's core.sparseCheckoutCone, which `git clone
	// --sparse` leaves unset on those versions — meaning cone mode is requested
	// but non-cone interpretation is what happens. setupSparseCheckout warns
	// when that's the case.
	modeEnforced bool
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
// build. It resolves to the zero value (a full checkout) when no paths were
// configured, and warns and does the same when git is too old to honour the
// requested mode. The only error is an unusable mode: that's job configuration
// we can't interpret, and failing the checkout beats guessing which files the
// job wanted.
func (e *Executor) resolveSparseCheckout(ctx context.Context) (sparseCheckout, error) {
	paths := cleanGitSparseCheckoutPaths(e.GitSparseCheckoutPaths)
	if len(paths) == 0 {
		return sparseCheckout{}, nil
	}

	mode, err := ParseSparseCheckoutMode(e.GitSparseCheckoutMode)
	if err != nil {
		return sparseCheckout{}, err
	}

	// Cone mode is available as soon as `sparse-checkout set` is, because git
	// takes the interpretation from the repository config when the mode option
	// is missing. Non-cone mode can only be requested via the option, so it
	// needs the higher floor; below it, fall back to a full checkout rather than
	// checking out the wrong files.
	required, requirement := minGitSparseCheckout, "Sparse checkout"
	if mode == SparseCheckoutModeNoCone {
		required, requirement = minGitSparseCheckoutMode, "Sparse checkout in no-cone mode"
	}

	version, err := gitVersion(ctx, e.shell)
	if err != nil {
		e.shell.Warningf("%s requires git >= %s; falling back to full checkout (%v)", requirement, required, err)
		return sparseCheckout{}, nil
	}
	if !version.atLeast(required) {
		e.shell.Warningf("%s requires git >= %s, got %s; falling back to full checkout", requirement, required, version)
		return sparseCheckout{}, nil
	}

	return sparseCheckout{
		paths:        paths,
		mode:         mode,
		modeEnforced: version.atLeast(minGitSparseCheckoutMode),
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
	if sc.modeEnforced {
		args = append(args, "--"+sc.mode.String())
	} else {
		e.shell.Warningf("This git is older than %s and can't be told which sparse checkout mode to use: the paths will be interpreted using the repository's existing core.sparseCheckoutCone setting, which may not be %s mode. Upgrade git to pin the mode.", minGitSparseCheckoutMode, sc.mode)
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
