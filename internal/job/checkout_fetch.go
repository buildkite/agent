package job

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/buildkite/agent/v3/internal/shell"
)

// fetchSource fetches the git source for the job. If GitSkipFetchExistingCommits is
// enabled and the commit already exists locally, the fetch is skipped entirely.
// When addBloblessFilter is true, --filter=blob:none is prepended to the fetch
// flags — the caller decides based on sparse-checkout state and user-supplied
// filters.
func (e *Executor) fetchSource(ctx context.Context, addBloblessFilter bool) error {
	// If configured, skip the fetch when the commit already exists locally.
	// This is useful when a pre-populated git mirror is used with --reference,
	// as the commit objects are already reachable and fetching is redundant.
	if e.GitSkipFetchExistingCommits && e.Commit != "HEAD" &&
		hasGitCommit(ctx, e.shell, ".git", e.Commit) {
		e.shell.Commentf("Commit %q already exists locally, skipping fetch", e.Commit)
		return nil
	}

	gitFetchFlags := e.GitFetchFlags
	if addBloblessFilter {
		gitFetchFlags = "--filter=blob:none " + gitFetchFlags
	}

	switch {
	case e.RefSpec != "":
		// If a refspec is provided then use it instead.
		// For example, `refs/not/a/head`
		e.shell.Commentf("Fetch and checkout custom refspec")
		if err := gitFetch(ctx, gitFetchArgs{
			Shell:         e.shell,
			GitFetchFlags: gitFetchFlags,
			Repository:    "origin",
			RefSpecs:      []string{e.RefSpec},
		}); err != nil {
			return fmt.Errorf("fetching refspec %q: %w", e.RefSpec, err)
		}

	case e.PullRequest != "false" && strings.Contains(e.PipelineProvider, "github"):
		var refspec string
		var retry bool

		if e.PullRequestUsingMergeRefspec {
			// Merge refspecs represents a speculative merge of the PR branch against the base branch.
			// Checking out this refspec enables testing the result of the merge before it happens.
			// If a merge conflict exists, this refspec won't be created and the fetch will fail. In this
			// case we want the job to fail earlier, rather than retrying the fetch (which adds ~2-3 mins job run time before failing)
			// Note: An outer retry loop will still retry the failed checkout 3 times before failing.
			e.shell.Commentf("Fetch and checkout pull request merge commit from GitHub")
			retry = false
			refspec = fmt.Sprintf("refs/pull/%s/merge", e.PullRequest)
		} else {
			// GitHub has a special ref which lets us fetch a pull request head, whether
			// or not it's a current head in this repository or a fork. See:
			// https://help.github.com/articles/checking-out-pull-requests-locally/#modifying-an-inactive-pull-request-locally
			e.shell.Commentf("Fetch and checkout pull request head from GitHub")
			retry = true
			refspec = fmt.Sprintf("refs/pull/%s/head", e.PullRequest)
		}
		refspecs := []string{refspec}

		if e.Commit == "HEAD" {
			// If we don't know the commit, we don't want to fetch with a fallback (otherwise FETCH_HEAD
			// will resolve during a fallback to the alphabetically earliest branch/tag - rather than the
			// correct commit for this build)
			if err := gitFetch(ctx, gitFetchArgs{
				Shell:         e.shell,
				GitFetchFlags: gitFetchFlags,
				Repository:    "origin",
				Retry:         retry,
				RefSpecs:      refspecs,
			}); err != nil {
				return fmt.Errorf("fetching PR refspec %q: %w", refspecs, err)
			}
		} else {
			// If we know the commit, also fetch it directly. The commit might not be in the history of `refspec` if there
			// have been force pushes to the pull request, so this ensures we have it.
			// Note: this is the typical case e.Commit != HEAD.
			refspecs = append(refspecs, e.Commit)
			// We aim to eliminate network round-trip as much as possible so we use a single git fetch here.
			if err := gitFetchWithFallback(ctx, e.shell, gitFetchFlags, refspecs...); err != nil {
				return fmt.Errorf("fetching PR refspec %q: %w", refspecs, err)
			}
		}

		gitFetchHead, _ := e.shell.Command("git", "rev-parse", "FETCH_HEAD").RunAndCaptureStdout(ctx)
		e.shell.Commentf("FETCH_HEAD is now `%s`", gitFetchHead)

	case e.Commit == "HEAD":
		// If the commit is "HEAD" then we can't do a commit-specific fetch and will
		// need to fetch the remote head and checkout the fetched head explicitly.
		e.shell.Commentf("Fetch and checkout remote branch HEAD commit")
		if err := gitFetch(ctx, gitFetchArgs{
			Shell:         e.shell,
			GitFetchFlags: gitFetchFlags,
			Repository:    "origin",
			RefSpecs:      []string{e.Branch},
		}); err != nil {
			return fmt.Errorf("fetching branch %q: %w", e.Branch, err)
		}

	default:
		// Otherwise fetch and checkout the commit directly.
		if err := gitFetchWithFallback(ctx, e.shell, gitFetchFlags, e.Commit); err != nil {
			return err
		}
	}

	return nil
}

// gitFetchWithFallback run git fetch for refspecs, when it fails on recoverable reason, it will retry fetching
// all heads and refs.
func gitFetchWithFallback(ctx context.Context, shell *shell.Shell, gitFetchFlags string, refspecs ...string) error {
	if len(refspecs) == 0 {
		return fmt.Errorf("no refspecs provided for git fetch")
	}

	// Try to fetch all refspecs in a single call first
	err := gitFetch(ctx, gitFetchArgs{
		Shell:         shell,
		GitFetchFlags: gitFetchFlags,
		Repository:    "origin",
		RefSpecs:      refspecs,
	})
	if err == nil {
		return nil // all refspecs worked in single fetch
	}

	if gerr := new(gitError); errors.As(err, &gerr) {
		switch gerr.Type {
		case gitErrorFetchBadReference:
			// refspecs might contains short SHA
			break
		default:
			// bail due to repository corruption or other non recoverable issue
			return fmt.Errorf("fetching refspecs %v: %w", refspecs, err)
		}
	}

	// The refspecs might be something that's not possible to fetch directly
	// (e.g. short commit hashes), so we fall back to fetching all heads and tags,
	// hoping that the refspecs are included.
	shell.Commentf("Some refspec fetches failed, trying to fetch all heads and tags")
	// By default `git fetch origin` will only fetch tags which are
	// reachable from a fetches branch. git 1.9.0+ changed `--tags` to
	// fetch all tags in addition to the default refspec, but pre 1.9.0 it
	// excludes the default refspec.
	gitFetchRefspec, err := shell.Command("git", "config", "remote.origin.fetch").RunAndCaptureStdout(ctx)
	if err != nil {
		return fmt.Errorf("getting remote.origin.fetch: %w", err)
	}

	if err := gitFetch(ctx, gitFetchArgs{
		Shell:         shell,
		GitFetchFlags: gitFetchFlags,
		Repository:    "origin",
		Retry:         true,
		RefSpecs:      []string{gitFetchRefspec, "+refs/tags/*:refs/tags/*"},
	}); err != nil {
		return fmt.Errorf("fetching refspecs %v: %w", refspecs, err)
	}

	return nil
}
