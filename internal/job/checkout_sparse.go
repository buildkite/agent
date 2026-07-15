package job

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/buildkite/agent/v3/internal/osutil"
	"github.com/buildkite/agent/v3/internal/shell"
)

// resolveSparseCheckout returns the cone paths to check out for this build, or
// nil to check out the full tree — either because no paths were requested or
// because git is too old (< 2.27).
func (e *Executor) resolveSparseCheckout(ctx context.Context) []string {
	paths := cleanGitSparseCheckoutPaths(e.GitSparseCheckoutPaths)
	if len(paths) == 0 {
		return nil
	}

	// We require git >= 2.27 because setupSparseCheckout runs
	// `git sparse-checkout set --cone <paths>`, which was promoted
	// from experimental to stable in git 2.27. On older git versions,
	// fall back to a full checkout by returning nil.
	ok, got, err := gitVersionAtLeast(ctx, e.shell, 2, 27)
	if err != nil {
		e.shell.Warningf("Sparse checkout requires git >= 2.27; falling back to full checkout (%v)", err)
		return nil
	}
	if !ok {
		e.shell.Warningf("Sparse checkout requires git >= 2.27, got %s; falling back to full checkout", got)
		return nil
	}
	return paths
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

func parseGitVersion(output string) (major, minor int, ok bool) {
	if _, err := fmt.Sscanf(output, "git version %d.%d", &major, &minor); err != nil {
		return 0, 0, false
	}
	return major, minor, true
}

// gitVersionAtLeast reports whether the local git binary is at least
// major.minor. It also returns the parsed "M.m" version string so callers can
// include it in log output. The err return is reserved for actual failures
// (git command failure, unparseable version output) — a git that is simply
// too old returns (false, "M.m", nil), not an error.
func gitVersionAtLeast(ctx context.Context, sh *shell.Shell, major, minor int) (ok bool, got string, err error) {
	output, err := sh.Command("git", "--version").RunAndCaptureStdout(ctx)
	if err != nil {
		return false, "", err
	}

	gitMajor, gitMinor, parseOK := parseGitVersion(strings.TrimSpace(output))
	if !parseOK {
		return false, "", fmt.Errorf("parsing git version from %q", strings.TrimSpace(output))
	}

	got = fmt.Sprintf("%d.%d", gitMajor, gitMinor)
	if gitMajor != major {
		return gitMajor > major, got, nil
	}
	return gitMinor >= minor, got, nil
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

// setupSparseCheckout configures git sparse checkout for the given cone paths.
// When sparsePaths is empty it does a full checkout instead, disabling any
// prior sparse checkout configuration. It returns true when sparse checkout is
// applied, so callers can skip steps that need the full tree (e.g. submodule
// init).
func (e *Executor) setupSparseCheckout(ctx context.Context, sparsePaths []string) (bool, error) {
	if len(sparsePaths) == 0 {
		e.disableSparseCheckoutIfConfigured(ctx)
		return false, nil
	}

	e.shell.Commentf("Setting up sparse checkout for paths: %s", strings.Join(sparsePaths, ","))
	args := append([]string{"sparse-checkout", "set", "--cone"}, sparsePaths...)
	if err := e.shell.Command("git", args...).Run(ctx); err != nil {
		return false, fmt.Errorf("setting sparse checkout paths: %w", err)
	}

	return true, nil
}
