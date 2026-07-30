package job

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/buildkite/agent/v3/internal/osutil"
)

const remoteMirrorAttemptTimeout = 30 * time.Second

var fullCommitSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

func isFullCommitSHA(commit string) bool {
	return fullCommitSHA.MatchString(commit)
}

func (e *Executor) shouldAttemptRemoteMirror(previousAttempts int) bool {
	return previousAttempts == 0 &&
		e.GitRemoteMirrorURL != "" &&
		isFullCommitSHA(e.Commit) &&
		e.RefSpec == "" &&
		e.Tag == "" &&
		e.PullRequest == "false"
}

// tryRemoteMirrorSource attempts source acquisition from the backend-provided
// mirror. It is deliberately bounded and never retries. The caller continues
// with the canonical source when this returns false.
func (e *Executor) tryRemoteMirrorSource(
	ctx context.Context,
	previousAttempts int,
	gitCloneFlags []string,
) (bool, error) {
	if !e.shouldAttemptRemoteMirror(previousAttempts) {
		return false, nil
	}

	span, mirrorCtx := e.traceOpSpan(ctx, "git.remote_mirror.attempt")
	startedAt := time.Now()
	result := "hit"
	fallback := false
	var spanErr error
	defer func() {
		span.AddAttributes(map[string]string{
			"git.remote_mirror.attempted": "true",
			"git.remote_mirror.result":    result,
			"git.remote_mirror.fallback":  fmt.Sprintf("%t", fallback),
			"git.remote_mirror.duration":  time.Since(startedAt).String(),
		})
		// Mirror errors are expected optimization misses. Only failure to
		// discard their checkout state is a checkout failure.
		span.FinishWithError(spanErr)
	}()

	mirrorCtx, cancel := context.WithTimeout(mirrorCtx, remoteMirrorTimeout(mirrorCtx))
	defer cancel()

	e.shell.Commentf("Attempting checkout from remote Git mirror")
	err := e.acquireRemoteMirrorSource(mirrorCtx, gitCloneFlags)
	if err == nil {
		return true, nil
	}

	fallback = true
	result = remoteMirrorResult(mirrorCtx, err)
	e.shell.Commentf("Remote Git mirror unavailable (%s); falling back to canonical repository", result)

	if cleanupErr := e.discardRemoteMirrorCheckout(ctx); cleanupErr != nil {
		result = "error"
		spanErr = fmt.Errorf("discarding remote mirror checkout state: %w", cleanupErr)
		return false, spanErr
	}
	return false, nil
}

func (e *Executor) acquireRemoteMirrorSource(ctx context.Context, gitCloneFlags []string) error {
	gitFlags := gitCredentialHelperFlags(ctx)
	existingGitDir := filepath.Join(e.shell.Getwd(), ".git")

	if osutil.FileExists(existingGitDir) {
		if _, err := e.updateRemoteURL(ctx, "", e.Repository); err != nil {
			return fmt.Errorf("setting canonical origin before mirror fetch: %w", err)
		}
		if err := gitFetch(ctx, gitFetchArgs{
			Shell:         e.shell,
			GitFlags:      gitFlags,
			GitFetchFlags: e.GitFetchFlags,
			Repository:    e.GitRemoteMirrorURL,
			Retry:         false,
			RefSpecs:      []string{e.Commit},
			Quiet:         true,
		}); err != nil {
			return fmt.Errorf("fetching exact commit from remote mirror: %w", err)
		}
	} else {
		mirrorCloneFlags := append(append([]string{}, gitCloneFlags...), "--no-checkout")
		if err := gitCloneWithArgs(ctx, gitCloneArgs{
			Shell:         e.shell,
			GitFlags:      gitFlags,
			GitCloneFlags: mirrorCloneFlags,
			Repository:    e.GitRemoteMirrorURL,
			Dir:           ".",
			Quiet:         true,
		}); err != nil {
			return fmt.Errorf("cloning remote mirror: %w", err)
		}
		if err := gitFetch(ctx, gitFetchArgs{
			Shell:         e.shell,
			GitFlags:      gitFlags,
			GitFetchFlags: e.GitFetchFlags,
			Repository:    e.GitRemoteMirrorURL,
			Retry:         false,
			RefSpecs:      []string{e.Commit},
			Quiet:         true,
		}); err != nil {
			return fmt.Errorf("fetching exact commit from remote mirror: %w", err)
		}
		if err := e.shell.Command("git", "remote", "set-url", "origin", e.Repository).Run(ctx); err != nil {
			return fmt.Errorf("setting canonical origin after mirror clone: %w", err)
		}
		if err := removeGitRefs(ctx, e, "refs/heads", "refs/remotes", "refs/tags"); err != nil {
			return fmt.Errorf("discarding refs advertised by remote mirror: %w", err)
		}
	}

	if !hasGitCommit(ctx, e.shell, ".git", e.Commit) {
		return errors.New("exact commit is not present after remote mirror fetch")
	}
	return nil
}

// discardRemoteMirrorCheckout removes all state from a failed optimization
// attempt without using the canonical checkout's long Windows retry loop.
// Retries are short and context-aware so canonical fallback keeps its budget.
func (e *Executor) discardRemoteMirrorCheckout(ctx context.Context) error {
	checkoutPath, _ := e.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH")
	if e.checkoutRoot != nil {
		_ = e.checkoutRoot.Close()
		e.checkoutRoot = nil
	}

	var lastErr error
	for range 3 {
		e.shell.Commentf("Removing %s", checkoutPath)
		if err := os.RemoveAll(checkoutPath); err != nil {
			lastErr = err
		} else if _, err := os.Stat(checkoutPath); os.IsNotExist(err) {
			if err := e.createCheckoutDir(); err != nil {
				return fmt.Errorf("recreating checkout directory: %w", err)
			}
			return nil
		} else {
			lastErr = fmt.Errorf("checkout directory still exists after removal")
		}

		select {
		case <-ctx.Done():
			return errors.Join(lastErr, ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
	return lastErr
}

// remoteMirrorTimeout reserves most of a checkout attempt's remaining deadline
// for canonical fallback. Without a parent deadline the mirror gets a fixed,
// bounded initial budget.
func remoteMirrorTimeout(ctx context.Context) time.Duration {
	timeout := remoteMirrorAttemptTimeout
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if reserved := remaining / 3; reserved < timeout {
			timeout = reserved
		}
	}
	if timeout <= 0 {
		return time.Nanosecond
	}
	return timeout
}

func removeGitRefs(ctx context.Context, e *Executor, prefixes ...string) error {
	args := append([]string{"for-each-ref", "--format=%(refname)"}, prefixes...)
	output, err := e.shell.Command("git", args...).RunAndCaptureStdout(ctx)
	if err != nil {
		return err
	}
	for ref := range strings.SplitSeq(strings.TrimSpace(output), "\n") {
		if ref == "" {
			continue
		}
		if err := e.shell.Command("git", "update-ref", "-d", ref).Run(ctx); err != nil {
			return err
		}
	}
	return nil
}

func remoteMirrorResult(ctx context.Context, err error) string {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	var gitErr *gitError
	if errors.As(err, &gitErr) && (gitErr.Type == gitErrorFetchBadReference || gitErr.Type == gitErrorFetchRefNotOnRemote) {
		return "miss"
	}
	return "error"
}
