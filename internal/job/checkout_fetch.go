package job

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/buildkite/agent/v3/internal/shell"
	"github.com/buildkite/shellwords"
)

const remoteMirrorPromisorMarkerKey = "buildkite.remote-mirror-promisor"

// refspecKind is the category of git refspec a fetch targets.
type refspecKind string

const (
	// e.RefSpec is set, overriding all other fetch behaviour
	refspecCustom refspecKind = "custom"

	// GitHub PR build using the speculative merge ref (refs/pull/N/merge)
	refspecGithubPRMerge refspecKind = "github-pr-merge"

	// GitHub PR build using the PR's head ref (refs/pull/N/head)
	refspecGithubPRHead refspecKind = "github-pr-head"

	// No specific commit is known (e.Commit == "HEAD"), so fetch the branch's remote HEAD
	refspecBranch refspecKind = "branch"

	// Default: a specific commit is known, so fetch and checkout it directly
	refspecCommit refspecKind = "commit"
)

// fetchSource fetches the git source for the job. If GitSkipFetchExistingCommits is
// enabled and the commit already exists locally, the fetch is skipped entirely.
// When addBloblessFilter is true, --filter=blob:none is prepended to the fetch
// flags — the caller decides based on sparse-checkout state and user-supplied
// filters.
func (e *Executor) fetchSource(ctx context.Context, addBloblessFilter bool, attempt *remoteMirrorAttempt) (retErr error) {
	// Start span here so attributes can be set on the in-scope span; covers the
	// whole fetch including retries (up to 10 attempts, ~2m), not per-attempt.
	span, ctx := e.traceOpSpan(ctx, "git.fetch")
	defer func() { span.FinishWithError(retErr) }()

	// Classify the refspec kind once and dispatch on it in the switch below.
	kind := refspecCommit
	switch {
	case e.RefSpec != "":
		kind = refspecCustom
	case e.PullRequest != "false" && strings.Contains(e.PipelineProvider, "github"):
		if e.PullRequestUsingMergeRefspec {
			kind = refspecGithubPRMerge
		} else {
			kind = refspecGithubPRHead
		}
	case e.Commit == "HEAD":
		kind = refspecBranch
	}

	span.AddAttributes(map[string]string{
		"git.pull_request": strconv.FormatBool(e.PullRequest != "false"),
		"git.refspec_kind": string(kind),
	})

	if err := e.repairInterruptedRemoteMirrorPromisors(ctx); err != nil {
		return &gitError{
			error: fmt.Errorf("repairing interrupted remote mirror promisor state: %w", err),
			Type:  gitErrorFetchRetryClean,
		}
	}

	// If configured, skip the fetch when the commit already exists locally.
	// This is useful when a pre-populated git mirror is used with --reference,
	// as the commit objects are already reachable and fetching is redundant.
	mirrorHit := attempt != nil && attempt.hit()
	skipFetch := (e.GitSkipFetchExistingCommits || mirrorHit) && e.Commit != "HEAD" &&
		hasGitCommit(ctx, e.shell, ".git", e.Commit)

	span.AddAttributes(map[string]string{"git.skipped": strconv.FormatBool(skipFetch)})

	if skipFetch {
		e.shell.Commentf("Commit %q already exists locally, skipping fetch", e.Commit)
		return nil
	}

	gitFetchFlags := e.GitFetchFlags
	if addBloblessFilter {
		gitFetchFlags = "--filter=blob:none " + gitFetchFlags
	}

	switch kind {
	case refspecCustom:
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

	case refspecGithubPRMerge, refspecGithubPRHead:
		var refspec string

		if kind == refspecGithubPRMerge {
			// Merge refspecs represents a speculative merge of the PR branch against the base branch.
			// Checking out this refspec enables testing the result of the merge before it happens.
			// If a merge conflict exists, this refspec won't be created and the fetch will fail. In this
			// case we want the job to fail earlier, rather than retrying the fetch (which adds ~2-3 mins job run time before failing)
			// Note: An outer retry loop will still retry the failed checkout 3 times before failing.
			e.shell.Commentf("Fetch and checkout pull request merge commit from GitHub")
			refspec = fmt.Sprintf("refs/pull/%s/merge", e.PullRequest)
		} else {
			// GitHub has a special ref which lets us fetch a pull request head, whether
			// or not it's a current head in this repository or a fork. See:
			// https://help.github.com/articles/checking-out-pull-requests-locally/#modifying-an-inactive-pull-request-locally
			e.shell.Commentf("Fetch and checkout pull request head from GitHub")
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
				// Retry:
				// GithubPRHead failures are retriable as they are usually transient network errors
				// GithubPRMmerge failures are not worth retrying as they are usually real merge conflicts
				Retry:    kind == refspecGithubPRHead,
				RefSpecs: refspecs,
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

	case refspecBranch:
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

	default: // refspecCommit
		if isExistingCheckoutRemoteMirrorAttempt(attempt) {
			mirrorFetchFlags, err := e.prepareRemoteMirrorFetch(ctx, gitFetchFlags)
			if err != nil {
				return fmt.Errorf("preparing remote mirror fetch: %w", err)
			}
			hit, err := e.fetchExistingCheckoutFromRemoteMirror(ctx, attempt, mirrorFetchFlags)
			if err != nil {
				return err
			}
			if hit {
				// C4: FETCH_HEAD records the non-secret mirror URL. No
				// mirror-eligible flow reads it, so retain Git's normal write.
				return nil
			}
		}

		// Otherwise fetch and checkout the commit directly.
		e.shell.Commentf("Fetch and checkout commit")
		if err := gitFetchWithFallback(ctx, e.shell, gitFetchFlags, e.Commit); err != nil {
			return fmt.Errorf("fetching commit %q: %w", e.Commit, err)
		}
	}

	return nil
}

func isExistingCheckoutRemoteMirrorAttempt(attempt *remoteMirrorAttempt) bool {
	return attempt != nil &&
		attempt.site == remoteMirrorSiteExistingCheckout &&
		attempt.outcome == remoteMirrorOutcomeNotReached
}

func (e *Executor) fetchExistingCheckoutFromRemoteMirror(
	ctx context.Context,
	attempt *remoteMirrorAttempt,
	gitFetchFlags string,
) (bool, error) {
	if err := e.markRemoteMirrorPromisor(ctx, attempt.url); err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		attempt.outcome = remoteMirrorOutcomeError
		e.shell.Warningf("Could not mark remote Git mirror state; falling back to canonical")
		return false, nil
	}

	e.shell.Commentf("Fetch commit from remote Git mirror")
	// C21: preserve the caller's effective fetch flags. Explicit ref-mutating
	// flags such as --tags may reflect the mirror's lagging view, while the
	// build commit remains the backend-provided immutable object ID.
	hit, fetchErr := e.fetchCommitFromRemoteMirror(
		ctx,
		attempt,
		".git",
		gitFetchFlags,
		e.Commit,
	)
	// Cleanup is bounded but deliberately detached: cancellation can race with
	// any individual config command, and leaving the one-shot mirror as a
	// promisor is not a safe cancellation state.
	cleanupCtx, cancelCleanup := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	cleanupErr := e.finishRemoteMirrorPromisor(cleanupCtx, attempt.url)
	cancelCleanup()
	if cleanupErr != nil {
		return false, &gitError{
			error: fmt.Errorf(
				"cleaning up unsafe remote mirror promisor state: %w",
				errors.Join(fetchErr, cleanupErr),
			),
			Type: gitErrorFetchRetryClean,
		}
	}
	return hit, fetchErr
}

func remoteMirrorPromisorKey(mirrorURL, name string) string {
	return "remote." + mirrorURL + "." + name
}

func remoteMirrorPromisorMarker(mirrorURL string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(mirrorURL)))
}

func (e *Executor) markRemoteMirrorPromisor(ctx context.Context, mirrorURL string) error {
	return e.runSilentLocalGitConfig(
		ctx,
		remoteMirrorPromisorMarkerKey,
		remoteMirrorPromisorMarker(mirrorURL),
	)
}

func (e *Executor) clearRemoteMirrorPromisorMarker(ctx context.Context) error {
	_, marked, err := e.localGitConfigValue(ctx, remoteMirrorPromisorMarkerKey)
	if err != nil || !marked {
		return err
	}
	return e.runSilentLocalGitConfig(ctx, "--unset-all", remoteMirrorPromisorMarkerKey)
}

func (e *Executor) prepareRemoteMirrorFetch(
	ctx context.Context,
	gitFetchFlags string,
) (string, error) {
	parts, err := shellwords.Split(gitFetchFlags)
	if err != nil {
		return "", err
	}
	if hasPartialFilterFlags(parts) || hasNoPartialFilterFlag(parts) {
		return gitFetchFlags, nil
	}
	filter, hasFilter, err := e.localGitConfigValue(ctx, "remote.origin.partialclonefilter")
	if err != nil {
		return "", err
	}
	if !hasFilter || filter == "" {
		return gitFetchFlags, nil
	}
	return strings.TrimSpace(gitFetchFlags + " " + shellwords.Quote("--filter="+filter)), nil
}

func hasNoPartialFilterFlag(flags []string) bool {
	return slices.ContainsFunc(flags, func(flag string) bool {
		return strings.HasPrefix(flag, "--no-fi")
	})
}

func (e *Executor) finishRemoteMirrorPromisor(
	ctx context.Context,
	mirrorURL string,
) error {
	marker, marked, err := e.localGitConfigValue(ctx, remoteMirrorPromisorMarkerKey)
	if err != nil {
		return fmt.Errorf("reading remote mirror promisor marker: %w", err)
	}
	if !marked || marker != remoteMirrorPromisorMarker(mirrorURL) {
		return nil
	}

	promisorKey := remoteMirrorPromisorKey(mirrorURL, "promisor")
	promisor, hasPromisor, err := e.localGitConfigValue(ctx, promisorKey)
	if err != nil {
		return fmt.Errorf("reading remote mirror promisor config: %w", err)
	}
	if !hasPromisor || promisor == "" {
		return e.clearRemoteMirrorPromisorMarker(ctx)
	}

	filterKey := remoteMirrorPromisorKey(mirrorURL, "partialclonefilter")
	filter, hasFilter, err := e.localGitConfigValue(ctx, filterKey)
	if err != nil {
		return fmt.Errorf("reading remote mirror partial-clone config: %w", err)
	}

	// A filtered fetch can write missing objects before it reports failure or
	// cancellation. Configure canonical ownership before removing mirror
	// ownership on every outcome so an interruption cannot strand those objects.
	if err := e.shell.Command(
		"git", "config", "--local", "remote.origin.promisor", "true",
	).Run(ctx, shell.AlwaysHidePrompt(), shell.ShowStderr(false)); err != nil {
		return fmt.Errorf("configuring origin promisor: %w", err)
	}
	if hasFilter {
		if err := e.shell.Command(
			"git", "config", "--local", "remote.origin.partialclonefilter", filter,
		).Run(ctx, shell.AlwaysHidePrompt(), shell.ShowStderr(false)); err != nil {
			return fmt.Errorf("configuring origin partial clone filter: %w", err)
		}
	}

	if err := e.runSilentLocalGitConfig(ctx, "--remove-section", "remote."+mirrorURL); err != nil {
		return fmt.Errorf("removing remote mirror promisor config: %w", err)
	}

	return e.clearRemoteMirrorPromisorMarker(ctx)
}

func (e *Executor) repairInterruptedRemoteMirrorPromisors(ctx context.Context) error {
	gitPath := filepath.Join(e.shell.Getwd(), ".git")
	gitInfo, err := os.Stat(gitPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if gitInfo.IsDir() {
		config, err := os.ReadFile(filepath.Join(gitPath, "config"))
		if err != nil {
			return err
		}
		// Avoid adding a Git invocation to checkouts the agent did not mark.
		if !strings.Contains(string(config), "remote-mirror-promisor") {
			return nil
		}
	}

	marker, marked, err := e.localGitConfigValue(ctx, remoteMirrorPromisorMarkerKey)
	if err != nil {
		return err
	}
	if !marked {
		return nil
	}

	output, err := e.runSilentLocalGitConfigOutput(ctx, "--get-regexp", `^remote\..*\.promisor$`)
	if err != nil {
		if shell.IsExitError(err) && shell.ExitCode(err) == 1 {
			return e.clearRemoteMirrorPromisorMarker(ctx)
		}
		return err
	}
	for line := range strings.SplitSeq(output, "\n") {
		key, _, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok {
			continue
		}
		remoteName := strings.TrimSuffix(strings.TrimPrefix(key, "remote."), ".promisor")
		remoteURL, err := url.Parse(remoteName)
		if err != nil || (remoteURL.Scheme != "https" && remoteURL.Scheme != "http") || remoteURL.Host == "" {
			continue
		}
		if remoteMirrorPromisorMarker(remoteName) != marker {
			continue
		}
		if err := e.finishRemoteMirrorPromisor(ctx, remoteName); err != nil {
			return err
		}
		return nil
	}
	return e.clearRemoteMirrorPromisorMarker(ctx)
}

func (e *Executor) localGitConfigValue(ctx context.Context, key string) (string, bool, error) {
	value, err := e.runSilentLocalGitConfigOutput(ctx, "--get", key)
	if err != nil {
		if shell.IsExitError(err) && shell.ExitCode(err) == 1 {
			return "", false, nil
		}
		return "", false, err
	}
	return strings.TrimSpace(value), true, nil
}

func (e *Executor) runSilentLocalGitConfig(ctx context.Context, args ...string) error {
	_, err := e.runSilentLocalGitConfigOutput(ctx, args...)
	return err
}

func (e *Executor) runSilentLocalGitConfigOutput(ctx context.Context, args ...string) (string, error) {
	commandArgs := append([]string{"config", "--local"}, args...)
	gitPath, err := e.shell.AbsolutePath("git")
	if err != nil {
		return "", errors.New("resolving git for silent config command")
	}
	cmd := exec.CommandContext(ctx, gitPath, commandArgs...)
	cmd.Dir = e.shell.Getwd()
	cmd.Env = e.shell.Env.ToSlice()
	output, err := cmd.Output()
	if err != nil {
		// Do not return argv or stderr: mirror-keyed config includes the URL.
		return "", fmt.Errorf("silent git config failed: %w", err)
	}
	return string(output), nil
}

// gitFetchWithFallback runs git fetch for refspecs; when it fails for a recoverable reason, it retries by fetching
// all heads and tags.
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
			// refspecs might contain a short SHA
			break
		default:
			// bail due to repository corruption or other unrecoverable issue
			return fmt.Errorf("fetching refspecs %v: %w", refspecs, err)
		}
	}

	// The refspecs might be something that's not possible to fetch directly
	// (e.g. short commit hashes), so we fall back to fetching all heads and tags,
	// hoping that the refspecs are included.
	shell.Commentf("Some refspec fetches failed, trying to fetch all heads and tags")
	// By default `git fetch origin` will only fetch tags which are
	// reachable from a fetched branch. git 1.9.0+ changed `--tags` to
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
