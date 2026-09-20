package job

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/buildkite/agent/v4/env"
	"github.com/buildkite/agent/v4/internal/shell"
)

// finishMirrorUpdate runs while the caller holds the mirror's clone or update
// lock, before creating a snapshot or cloning the workspace. LFS prefetching is
// optional: the workspace still fetches with its own configuration and retries,
// reusing cached objects where possible.
func (e *Executor) finishMirrorUpdate(ctx context.Context, repository, mirrorDir, lfsRef string) (string, error) {
	if e.GitLFSEnabled && repository == e.Repository {
		if err := e.prefetchMirrorLFS(ctx, mirrorDir, lfsRef); err != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			e.shell.Warningf("Unable to prefetch Git LFS objects into mirror; deferring to checkout: %v", err)
		}
	}
	return e.snapshotMirror(ctx, repository, mirrorDir)
}

// mirrorLFSRef selects the job revision in a freshly cloned mirror, which has
// no job-specific FETCH_HEAD. HEAD means the requested build ref, not the
// mirror's default branch. Existing mirrors use the fresh fetch result instead.
func (e *Executor) mirrorLFSRef() string {
	if e.Commit != "HEAD" {
		return e.Commit
	}
	switch {
	case e.RefSpec != "":
		// Only the source side names an object in a fresh mirror.
		ref, _, _ := strings.Cut(strings.TrimPrefix(e.RefSpec, "+"), ":")
		return ref
	case e.PullRequest != "false" && strings.Contains(e.PipelineProvider, "github"):
		if e.PullRequestUsingMergeRefspec {
			return fmt.Sprintf("refs/pull/%s/merge", e.PullRequest)
		}
		return fmt.Sprintf("refs/pull/%s/head", e.PullRequest)
	default:
		return e.Branch
	}
}

func (e *Executor) prefetchMirrorLFS(ctx context.Context, mirrorDir, ref string) error {
	if ref == "" {
		return nil
	}
	// Resolve once while the mirror lock is held, then use the same commit
	// for both configuration and objects. Unresolvable refs defer to
	// the workspace, which may also see a newer tip if the ref moves later.
	commit, err := e.shell.Command("git", "--git-dir", mirrorDir, "rev-parse", "--verify", ref+"^{commit}").RunAndCaptureStdout(ctx,
		shell.ShowStderr(false),
		shell.WithExtraEnv(env.FromSlice([]string{"GIT_NO_LAZY_FETCH=1"})),
	)
	if err != nil {
		return ctx.Err()
	}
	commit = strings.TrimSpace(commit)

	storage, err := e.shell.Command("git", "--git-dir", mirrorDir, "config", "--get", "--default", "", "lfs.storage").RunAndCaptureStdout(ctx)
	if err != nil {
		return fmt.Errorf("reading mirror LFS storage configuration: %w", err)
	}
	if strings.TrimSpace(storage) != "" {
		// LFS alternates discover only the default mirror/lfs storage layout.
		return nil
	}

	sparse, err := e.resolveSparseCheckout(ctx)
	if err != nil {
		return err
	}

	// LFS normally reads .lfsconfig from the worktree, index, or HEAD. The
	// mirror's HEAD may be another branch. Provide only the job's .lfsconfig
	// in a temporary worktree, letting LFS apply its normal safe-key filtering.
	// An empty file also prevents fallback to mirror HEAD when this commit
	// has no .lfsconfig. No mirror refs, index, or config are changed.
	configDir, err := os.MkdirTemp("", "buildkite-lfs-config-")
	if err != nil {
		return fmt.Errorf("creating LFS configuration directory: %w", err)
	}
	defer os.RemoveAll(configDir) //nolint:errcheck // Best-effort temporary config cleanup.

	configPath, err := e.shell.Command("git", "--git-dir", mirrorDir, "ls-tree", "--name-only", commit, "--", ".lfsconfig").RunAndCaptureStdout(ctx)
	if err != nil {
		return fmt.Errorf("finding job LFS configuration: %w", err)
	}
	var config string
	if strings.TrimSpace(configPath) != "" {
		config, err = e.shell.Command("git", "--git-dir", mirrorDir, "show", commit+":.lfsconfig").RunAndCaptureStdout(ctx)
		if err != nil {
			return fmt.Errorf("reading job LFS configuration: %w", err)
		}
	}
	if err := os.WriteFile(filepath.Join(configDir, ".lfsconfig"), []byte(config), 0o600); err != nil {
		return fmt.Errorf("writing job LFS configuration: %w", err)
	}

	args := []string{"--git-dir", mirrorDir, "--work-tree", configDir, "lfs", "fetch"}
	if include := sparse.lfsInclude(); len(include) > 0 {
		args = append(args, "--include="+strings.Join(include, ","))
	}
	args = append(args, "origin", commit)
	e.shell.Commentf("Prefetching Git LFS objects into mirror %q", mirrorDir)
	return e.traceOp(ctx, "git.mirror.lfs.fetch", func(ctx context.Context) error {
		return e.shell.Command("git", args...).Run(ctx)
	})
}
