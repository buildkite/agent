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
	"github.com/buildkite/agent/v3/internal/shell"
	"github.com/buildkite/shellwords"
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

	cloneConfigs, ok := remoteMirrorCompatibleCloneConfigs(gitCloneFlags)
	if !ok {
		return false, nil
	}

	existingGitDir := filepath.Join(e.shell.Getwd(), ".git")
	if osutil.FileExists(existingGitDir) && e.isPartialCloneCheckout(ctx) {
		// Fetch negotiation against an existing promisor remote can treat
		// promised objects as complete without backfilling blobs.
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
		// discard newly created checkout state is a checkout failure.
		span.FinishWithError(spanErr)
	}()

	mirrorCtx, cancel := context.WithTimeout(mirrorCtx, remoteMirrorTimeout(mirrorCtx))
	defer cancel()

	// Fetching an already-local SHA can succeed even when the remote does not
	// have the object. Probe without promisor lazy-fetches from origin, which
	// would otherwise escape this attempt's timeout on partial clones.
	if alreadyLocal, err := e.hasLocalCommitWithoutLazyFetch(mirrorCtx, e.Commit); err != nil {
		fallback = true
		result = remoteMirrorResult(mirrorCtx, err)
		e.shell.Commentf("Remote Git mirror unavailable (%s); falling back to canonical repository", result)
		return false, nil
	} else if alreadyLocal {
		result = "local"
		return false, nil
	}

	e.shell.Commentf("Attempting checkout from remote Git mirror")
	createdCheckout, err := e.acquireRemoteMirrorSource(mirrorCtx, cloneConfigs)
	if err == nil {
		return true, nil
	}

	fallback = true
	result = remoteMirrorResult(mirrorCtx, err)
	e.shell.Commentf("Remote Git mirror unavailable (%s); falling back to canonical repository", result)

	// Only discard state the mirror attempt created. A failed fetch into an
	// existing checkout leaves a valid repository for the canonical path.
	if createdCheckout {
		if cleanupErr := e.discardRemoteMirrorCheckout(ctx); cleanupErr != nil {
			result = "error"
			spanErr = fmt.Errorf("discarding remote mirror checkout state: %w", cleanupErr)
			return false, spanErr
		}
	}
	return false, nil
}

func (e *Executor) isPartialCloneCheckout(ctx context.Context) bool {
	// Filtered clones are marked by remote.<name>.promisor /
	// remote.<name>.partialclonefilter, not extensions.partialclone.
	quiet := []shell.RunCommandOpt{shell.ShowPrompt(false), shell.ShowStderr(false)}
	for _, pattern := range []string{`^remote\..*\.promisor$`, `^remote\..*\.partialclonefilter$`} {
		out, err := e.shell.Command(
			"git", "config", "--local", "--get-regexp", pattern,
		).RunAndCaptureStdout(ctx, quiet...)
		if err == nil && strings.TrimSpace(out) != "" {
			return true
		}
	}
	return false
}

// hasLocalCommitWithoutLazyFetch reports whether commit is already present
// locally without contacting promisor remotes.
func (e *Executor) hasLocalCommitWithoutLazyFetch(ctx context.Context, commit string) (bool, error) {
	previous, hadPrevious := e.shell.Env.Get("GIT_NO_LAZY_FETCH")
	e.shell.Env.Set("GIT_NO_LAZY_FETCH", "1")
	defer func() {
		if hadPrevious {
			e.shell.Env.Set("GIT_NO_LAZY_FETCH", previous)
			return
		}
		e.shell.Env.Remove("GIT_NO_LAZY_FETCH")
	}()

	if err := ctx.Err(); err != nil {
		return false, err
	}
	return hasGitCommit(ctx, e.shell, ".git", commit), nil
}

// acquireRemoteMirrorSource fetches the exact commit from the remote mirror
// into either an existing checkout or a freshly initialized repository with
// the canonical origin already configured. It returns whether this attempt
// created the checkout directory's .git state.
//
// The fetch strips partial-clone filters. A filtered fetch would register the
// one-shot mirror URL as a promisor remote in .git/config, and later lazy blob
// fetches would escape this attempt's timeout and credential scope.
func (e *Executor) acquireRemoteMirrorSource(ctx context.Context, cloneConfigs [][2]string) (bool, error) {
	gitFlags := gitCredentialHelperFlags(ctx)
	fetchFlags, err := remoteMirrorFetchFlags(e.GitFetchFlags)
	if err != nil {
		return false, err
	}
	existingGitDir := filepath.Join(e.shell.Getwd(), ".git")
	createdCheckout := !osutil.FileExists(existingGitDir)
	quiet := []shell.RunCommandOpt{shell.ShowPrompt(false), shell.ShowStderr(false)}

	if createdCheckout {
		if err := e.shell.Command("git", "init").Run(ctx, quiet...); err != nil {
			return true, fmt.Errorf("initializing repository for remote mirror: %w", err)
		}
		if err := e.shell.Command("git", "remote", "add", "origin", e.Repository).Run(ctx, quiet...); err != nil {
			return true, fmt.Errorf("adding canonical origin for remote mirror: %w", err)
		}
	} else if _, err := e.updateRemoteURL(ctx, "", e.Repository); err != nil {
		return false, fmt.Errorf("setting canonical origin before mirror fetch: %w", err)
	}

	if err := gitFetch(ctx, gitFetchArgs{
		Shell:         e.shell,
		GitFlags:      gitFlags,
		GitFetchFlags: fetchFlags,
		Repository:    e.GitRemoteMirrorURL,
		Retry:         false,
		RefSpecs:      []string{e.Commit},
		Quiet:         true,
	}); err != nil {
		return createdCheckout, fmt.Errorf("fetching exact commit from remote mirror: %w", err)
	}

	if !hasGitCommit(ctx, e.shell, ".git", e.Commit) {
		return createdCheckout, errors.New("exact commit is not present after remote mirror fetch")
	}

	if createdCheckout {
		if err := e.applyRemoteMirrorCloneConfigs(ctx, cloneConfigs, quiet); err != nil {
			return true, err
		}
	}
	return createdCheckout, nil
}

// applyRemoteMirrorCloneConfigs persists git clone --config values into a
// checkout created by the mirror path.
//
// These values are supplied for the canonical repository, so they are applied
// only after the mirror fetch: keys such as http.extraHeader are read by Git
// immediately and would otherwise send canonical clone credentials to the
// mirror host. Applying them here still makes them cover the checkout and any
// later canonical transport, as git clone --config does.
func (e *Executor) applyRemoteMirrorCloneConfigs(ctx context.Context, cloneConfigs [][2]string, quiet []shell.RunCommandOpt) error {
	for _, kv := range cloneConfigs {
		// git clone --config is additive for repeated keys, so use --add to
		// preserve every value (e.g. multiple http.extraHeader).
		if err := e.shell.Command("git", "config", "--add", kv[0], kv[1]).Run(ctx, quiet...); err != nil {
			return fmt.Errorf("applying git clone --config %s=%s for remote mirror: %w", kv[0], kv[1], err)
		}
	}
	return nil
}

// remoteMirrorCompatibleCloneConfigs returns --config key/value pairs that can
// be applied after git init. It reports false when clone flags include options
// whose semantics cannot be reproduced on the init+fetch mirror path.
func remoteMirrorCompatibleCloneConfigs(gitCloneFlags []string) ([][2]string, bool) {
	var configs [][2]string
	for i := 0; i < len(gitCloneFlags); i++ {
		flag := gitCloneFlags[i]
		switch {
		case flag == "-v", flag == "--verbose", flag == "-q", flag == "--quiet", flag == "--no-checkout":
			continue
		case isGitFilterFlag(flag), flag == "--sparse":
			// Partial/sparse clone options are not reproducible here; skip the
			// optimization so the canonical path owns those semantics.
			return nil, false
		case flag == "--config":
			if i+1 >= len(gitCloneFlags) {
				return nil, false
			}
			i++
			config, ok := parseCloneConfig(gitCloneFlags[i])
			if !ok {
				return nil, false
			}
			configs = append(configs, config)
		case strings.HasPrefix(flag, "--config="):
			config, ok := parseCloneConfig(strings.TrimPrefix(flag, "--config="))
			if !ok {
				return nil, false
			}
			configs = append(configs, config)
		default:
			return nil, false
		}
	}
	return configs, true
}

// parseCloneConfig parses a git clone --config key=value argument, reporting
// false when the mirror path cannot reproduce clone's semantics for that key.
func parseCloneConfig(arg string) ([2]string, bool) {
	key, value, ok := strings.Cut(arg, "=")
	if !ok || key == "" || configuresOriginRemote(key) {
		return [2]string{}, false
	}
	return [2]string{key, value}, true
}

// configuresOriginRemote reports whether key configures the origin remote.
//
// git clone writes that remote itself and overwrites any --config for it, but
// the mirror path creates it with git remote add before adding clone config.
// An added remote.origin.url would therefore survive as a second value, which
// Git treats as an extra push destination.
//
// Config section and variable names are case-insensitive; the subsection (the
// remote name) is case-sensitive.
func configuresOriginRemote(key string) bool {
	section, rest, ok := strings.Cut(key, ".")
	if !ok || !strings.EqualFold(section, "remote") {
		return false
	}
	subsection, _, ok := strings.Cut(rest, ".")
	return ok && subsection == "origin"
}

func remoteMirrorFetchFlags(gitFetchFlags string) (string, error) {
	parts, err := shellwords.Split(gitFetchFlags)
	if err != nil {
		return "", fmt.Errorf("splitting git fetch flags for remote mirror: %w", err)
	}
	kept := make([]string, 0, len(parts))
	for i := 0; i < len(parts); i++ {
		part := parts[i]
		if isGitFilterFlag(part) {
			if part == "--filter" || isGitFilterAbbreviationWithoutValue(part) {
				if i+1 < len(parts) {
					i++
				}
			}
			continue
		}
		kept = append(kept, part)
	}
	return strings.Join(kept, " "), nil
}

func isGitFilterFlag(part string) bool {
	if part == "--filter" || strings.HasPrefix(part, "--filter=") {
		return true
	}
	// git accepts unambiguous abbreviations of --filter (e.g. --filt=blob:none).
	if strings.HasPrefix(part, "--fil") && !strings.HasPrefix(part, "--file") {
		return true
	}
	return false
}

func isGitFilterAbbreviationWithoutValue(part string) bool {
	return part != "--filter" && !strings.Contains(part, "=") && isGitFilterFlag(part)
}

// discardRemoteMirrorCheckout removes state created by a failed optimization
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
