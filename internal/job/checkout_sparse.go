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

func gitVersionAtLeast(ctx context.Context, sh *shell.Shell, major, minor int) (bool, error) {
	output, err := sh.Command("git", "--version").RunAndCaptureStdout(ctx)
	if err != nil {
		return false, err
	}

	gitMajor, gitMinor, ok := parseGitVersion(strings.TrimSpace(output))
	if !ok {
		return false, fmt.Errorf("parsing git version from %q", strings.TrimSpace(output))
	}

	if gitMajor != major {
		return gitMajor > major, nil
	}
	return gitMinor >= minor, nil
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

// enableCloneFlagsForSparseCheckout reports whether the caller should append
// --filter=blob:none to the clone flags for a sparse checkout. It returns
// false (with a comment explaining why) if the user has already supplied any
// --filter in BUILDKITE_GIT_CLONE_FLAGS, so we don't stack a second filter
// onto theirs — git takes the last --filter on the command line and would
// silently override the user's choice.
//
// Deliberately does NOT check the git version. Doing so would mean running
// `git --version` twice per checkout (once here before the clone, and once
// inside setupSparseCheckout after the fetch), which is wasted work for the
// common case where git is recent. If the local git turns out to be too old
// to support sparse checkout, the worst-case effect is a partial clone whose
// sparse-checkout step then falls back to a full checkout — the user gets
// the whole tree, just with --filter=blob:none objects fetched lazily on
// first access. setupSparseCheckout is the single source of truth for the
// version gate.
func (e *Executor) enableCloneFlagsForSparseCheckout(cloneFlags []string) bool {
	if len(cleanGitSparseCheckoutPaths(e.GitSparseCheckoutPaths)) == 0 {
		return false
	}

	if hasPartialCloneFilter(cloneFlags) {
		e.shell.Commentf("Sparse checkout is configured and BUILDKITE_GIT_CLONE_FLAGS already contains a --filter (preserving user-supplied filter).")
		return false
	}
	return true
}

// setupSparseCheckout configures (or disables) git sparse checkout for the
// current working tree. It returns true if sparse checkout was successfully
// applied for this build, so callers can adjust later behaviour (e.g. skip
// submodule init, which requires the full tree).
func (e *Executor) setupSparseCheckout(ctx context.Context) (bool, error) {
	paths := cleanGitSparseCheckoutPaths(e.GitSparseCheckoutPaths)
	if len(paths) == 0 {
		e.disableSparseCheckoutIfConfigured(ctx)
		return false, nil
	}

	// 2.27 is the floor because we use `git sparse-checkout set --cone <paths>`
	// in a single call below. Cone mode shipped in 2.26 but `--cone` was only
	// accepted on the `init` subcommand then; `set --cone` landed in 2.27. On
	// 2.26.x the `set --cone` call fails, so we fall back to a full checkout
	// rather than carrying the older `init --cone` + `set <paths>` two-step.
	ok, err := gitVersionAtLeast(ctx, e.shell, 2, 27)
	if err != nil {
		e.shell.Warningf("Sparse checkout requires git >= 2.27; falling back to full checkout (%v)", err)
		e.disableSparseCheckoutIfConfigured(ctx)
		return false, nil
	}
	if !ok {
		e.shell.Warningf("Sparse checkout requires git >= 2.27; falling back to full checkout")
		e.disableSparseCheckoutIfConfigured(ctx)
		return false, nil
	}

	e.shell.Commentf("Setting up sparse checkout for paths: %s", strings.Join(paths, ","))
	args := append([]string{"sparse-checkout", "set", "--cone"}, paths...)
	if err := e.shell.Command("git", args...).Run(ctx); err != nil {
		return false, fmt.Errorf("setting sparse checkout paths: %w", err)
	}

	return true, nil
}
