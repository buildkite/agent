package job

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/internal/osutil"
	"github.com/buildkite/agent/v3/internal/shell"
	"github.com/buildkite/shellwords"
)

const remoteMirrorAttemptTimeout = 30 * time.Second

var fullCommitSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

var remoteMirrorRepositoryRedirectEnv = []string{
	"GIT_DIR",
	"GIT_WORK_TREE",
	"GIT_COMMON_DIR",
	"GIT_OBJECT_DIRECTORY",
	"GIT_ALTERNATE_OBJECT_DIRECTORIES",
	"GIT_INDEX_FILE",
	"GIT_CONFIG",
	"GIT_SHALLOW_FILE",
	"GIT_NAMESPACE",
	"GIT_REPLACE_REF_BASE",
}

func isFullCommitSHA(commit string) bool {
	return fullCommitSHA.MatchString(commit)
}

func (e *Executor) shouldAttemptRemoteMirror(previousAttempts int) bool {
	return previousAttempts == 0 &&
		isSupportedRemoteMirrorURL(e.GitRemoteMirrorURL) &&
		isFullCommitSHA(e.Commit) &&
		e.RefSpec == "" &&
		e.Tag == "" &&
		e.PullRequest == "false"
}

func isSupportedRemoteMirrorURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return false
	}
	return u.Scheme == "https" || u.Scheme == "http"
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

	// These variables redirect Git away from the checkout being created.
	// Canonical git clone does not use them to choose its destination
	// repository, and allowing the mirror sequence to inherit them could make
	// it read credentials or objects from another checkout. Fall back rather
	// than trying to reinterpret user-supplied repository layout.
	if remoteMirrorRepositoryRedirected(e.shell.Env) {
		return false, nil
	}

	// Existing checkouts can contain canonical credentials and transport
	// configuration persisted by an earlier clone. Git reads that local config
	// before fetching, so the mirror trust boundary cannot be isolated there.
	// The canonical incremental fetch is already the efficient path for reuse.
	existingGitDir := filepath.Join(e.shell.Getwd(), ".git")
	if osutil.FileExists(existingGitDir) {
		return false, nil
	}

	plan, ok, err := planRemoteMirrorCheckout(gitCloneFlags, e.GitFetchFlags)
	if err != nil {
		return false, err
	}
	if !ok {
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

	e.shell.Commentf("Attempting checkout from remote Git mirror")
	createdCheckout, err := e.acquireRemoteMirrorSource(mirrorCtx, plan)
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

func remoteMirrorRepositoryRedirected(environ *env.Environment) bool {
	for _, name := range remoteMirrorRepositoryRedirectEnv {
		if environ.Exists(name) {
			return true
		}
	}
	return false
}

// acquireRemoteMirrorSource fetches the exact commit from the remote mirror
// into a freshly initialized repository with the canonical origin configured.
// Pipeline configuration that controls object selection is preserved, while
// canonical transport configuration is withheld from the mirror request.
func (e *Executor) acquireRemoteMirrorSource(ctx context.Context, plan remoteMirrorCheckoutPlan) (bool, error) {
	gitFlags := gitCredentialHelperFlags(ctx)
	for _, config := range plan.mirrorConfigs {
		gitFlags += " -c " + shellwords.Quote(config[0]+"="+config[1])
	}
	quiet := []shell.RunCommandOpt{shell.ShowPrompt(false), shell.ShowStderr(false)}
	fetchEnv := remoteMirrorFetchEnv()
	var originFetch string
	if plan.singleBranch {
		branch, err := e.remoteMirrorCanonicalHEAD(ctx, plan.cloneConfigs, quiet)
		if err != nil {
			// No repository state exists yet, so canonical checkout can proceed
			// without cleanup and reproduce git clone's handling of this HEAD.
			return false, fmt.Errorf("resolving canonical repository HEAD for remote mirror: %w", err)
		}
		originFetch = "+" + branch + ":refs/remotes/origin/" + strings.TrimPrefix(branch, "refs/heads/")
	}

	initOpts := append([]shell.RunCommandOpt{}, quiet...)
	initOpts = append(initOpts, shell.WithExtraEnv(fetchEnv))
	// Quiet the Git 2.x "Using 'master' as the name for the initial branch"
	// advice. Git 3.x defaults to main, so this becomes a no-op then.
	if err := e.shell.Command("git", "-c", "init.defaultBranch=main", "init").Run(ctx, initOpts...); err != nil {
		return true, fmt.Errorf("initializing repository for remote mirror: %w", err)
	}
	if err := e.shell.Command("git", "remote", "add", "origin", e.Repository).Run(ctx, quiet...); err != nil {
		return true, fmt.Errorf("adding canonical origin for remote mirror: %w", err)
	}
	if originFetch != "" {
		if err := e.shell.Command("git", "config", "--local", "remote.origin.fetch", originFetch).Run(ctx, quiet...); err != nil {
			return true, fmt.Errorf("configuring single-branch canonical fetch for remote mirror: %w", err)
		}
	}
	if plan.noTags {
		if err := e.shell.Command("git", "config", "--local", "remote.origin.tagOpt", "--no-tags").Run(ctx, quiet...); err != nil {
			return true, fmt.Errorf("configuring canonical no-tags fetch for remote mirror: %w", err)
		}
	}

	if err := gitFetch(ctx, gitFetchArgs{
		Shell:         e.shell,
		GitFlags:      gitFlags,
		GitFetchFlags: plan.fetchFlags,
		Repository:    e.GitRemoteMirrorURL,
		Retry:         false,
		RefSpecs:      []string{e.Commit},
		Quiet:         true,
		ExtraEnv:      fetchEnv,
	}); err != nil {
		return true, fmt.Errorf("fetching exact commit from remote mirror: %w", err)
	}

	if !hasGitCommit(ctx, e.shell, ".git", e.Commit) {
		return true, errors.New("exact commit is not present after remote mirror fetch")
	}

	if plan.partialClone {
		if err := e.retargetRemoteMirrorPromisor(ctx, quiet); err != nil {
			return true, err
		}
	}

	if err := e.applyRemoteMirrorCloneConfigs(ctx, plan.cloneConfigs, quiet); err != nil {
		return true, err
	}
	return true, nil
}

// remoteMirrorCanonicalHEAD resolves the canonical repository's default branch
// without transferring its object graph. A single-branch clone persists this
// branch in remote.origin.fetch, so mirror-backed clones need the canonical
// advertisement rather than the mirror's potentially stale HEAD.
func (e *Executor) remoteMirrorCanonicalHEAD(ctx context.Context, cloneConfigs [][2]string, opts []shell.RunCommandOpt) (string, error) {
	args := make([]string, 0, 2*len(cloneConfigs)+4)
	for _, config := range cloneConfigs {
		args = append(args, "-c", config[0]+"="+config[1])
	}
	args = append(args, "ls-remote", "--symref", e.Repository, "HEAD")
	out, err := e.shell.Command("git", args...).RunAndCaptureStdout(ctx, opts...)
	if err != nil {
		return "", err
	}
	for line := range strings.SplitSeq(out, "\n") {
		ref, target, ok := strings.Cut(line, "\t")
		if !ok || target != "HEAD" {
			continue
		}
		branch, ok := strings.CutPrefix(ref, "ref: ")
		if ok && strings.HasPrefix(branch, "refs/heads/") {
			return branch, nil
		}
	}
	return "", errors.New("canonical HEAD does not resolve to a branch")
}

// remoteMirrorFetchEnv hides ambient Git configuration from the mirror
// transport. Pipeline clone configuration is applied after acquisition, and
// only explicitly allowlisted mirror configuration is supplied with -c.
func remoteMirrorFetchEnv() *env.Environment {
	environ := env.New()
	environ.Set("GIT_CONFIG_NOSYSTEM", "1")
	environ.Set("GIT_CONFIG_SYSTEM", os.DevNull)
	environ.Set("GIT_CONFIG_GLOBAL", os.DevNull)
	environ.Set("GIT_CONFIG_COUNT", "0")
	environ.Set("GIT_CONFIG_PARAMETERS", "")
	environ.Set("GIT_TEMPLATE_DIR", "")
	return environ
}

// retargetRemoteMirrorPromisor makes later lazy object fetches use canonical
// origin rather than the one-shot mirror URL.
func (e *Executor) retargetRemoteMirrorPromisor(ctx context.Context, quiet []shell.RunCommandOpt) error {
	out, err := e.shell.Command(
		"git", "config", "--local", "--get-regexp", `^remote\..*\.(promisor|partialclonefilter)$`,
	).RunAndCaptureStdout(ctx, quiet...)
	if err != nil || strings.TrimSpace(out) == "" {
		// Servers may ignore filters they do not support, in which case Git
		// writes no promisor configuration and the fetched commit is complete.
		return nil
	}

	var filter string
	foundPromisor := false
	for line := range strings.SplitSeq(out, "\n") {
		key, value, ok := strings.Cut(line, " ")
		if !ok {
			return fmt.Errorf("parsing remote mirror promisor config %q", line)
		}
		switch {
		case strings.HasSuffix(strings.ToLower(key), ".promisor"):
			foundPromisor = true
		case strings.HasSuffix(strings.ToLower(key), ".partialclonefilter"):
			filter = value
		}
		if err := e.shell.Command("git", "config", "--local", "--unset-all", key).Run(ctx, quiet...); err != nil {
			return fmt.Errorf("removing remote mirror promisor config %s: %w", key, err)
		}
	}
	if !foundPromisor && filter == "" {
		return nil
	}
	if err := e.shell.Command("git", "config", "--local", "remote.origin.promisor", "true").Run(ctx, quiet...); err != nil {
		return fmt.Errorf("configuring canonical origin as promisor: %w", err)
	}
	if filter != "" {
		if err := e.shell.Command("git", "config", "--local", "remote.origin.partialclonefilter", filter).Run(ctx, quiet...); err != nil {
			return fmt.Errorf("configuring canonical origin partial clone filter: %w", err)
		}
	}
	return nil
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

type remoteMirrorCheckoutPlan struct {
	cloneConfigs  [][2]string
	mirrorConfigs [][2]string
	fetchFlags    string
	partialClone  bool
	singleBranch  bool
	noTags        bool
}

// planRemoteMirrorCheckout separates checkout semantics from mirror transport
// configuration. Data-shaping options are sent to the mirror so large
// repositories remain shallow/filtered, while canonical credentials and
// unknown transport controls never cross the mirror trust boundary.
func planRemoteMirrorCheckout(gitCloneFlags []string, gitFetchFlags string) (remoteMirrorCheckoutPlan, bool, error) {
	var plan remoteMirrorCheckoutPlan
	fetchParts, err := shellwords.Split(gitFetchFlags)
	if err != nil {
		return plan, false, fmt.Errorf("splitting git fetch flags for remote mirror: %w", err)
	}
	if !remoteMirrorFetchFlagsAreSafe(fetchParts) {
		return plan, false, nil
	}

	var cloneFetchOptions [][]string
	singleBranchSet := false
	for i := 0; i < len(gitCloneFlags); i++ {
		flag := gitCloneFlags[i]
		switch {
		case flag == "-v", flag == "--verbose", flag == "-q", flag == "--quiet", flag == "--no-checkout":
			continue
		case flag == "--sparse":
			// Sparse worktree setup happens after source acquisition. The
			// mirror fetch already requests only the exact immutable commit.
			continue
		case flag == "--single-branch":
			plan.singleBranch = true
			singleBranchSet = true
		case flag == "--no-single-branch":
			plan.singleBranch = false
			singleBranchSet = true
		case flag == "--no-tags":
			cloneFetchOptions = append(cloneFetchOptions, []string{flag})
			plan.noTags = true
		case isGitFilterFlag(flag):
			option, consumed, ok := gitOptionWithValue(gitCloneFlags, i, isGitFilterFlag)
			if !ok {
				return plan, false, nil
			}
			i += consumed
			cloneFetchOptions = append(cloneFetchOptions, option)
			plan.partialClone = true
		case isShallowCloneFlag(flag):
			option, consumed, ok := gitOptionWithValue(gitCloneFlags, i, isShallowCloneFlag)
			if !ok {
				return plan, false, nil
			}
			i += consumed
			cloneFetchOptions = append(cloneFetchOptions, option)
			if gitLongOption(flag, "--depth") && !singleBranchSet {
				plan.singleBranch = true
			}
		case flag == "--config":
			if i+1 >= len(gitCloneFlags) {
				return plan, false, nil
			}
			i++
			config, ok := parseCloneConfig(gitCloneFlags[i])
			if !ok {
				return plan, false, nil
			}
			plan.cloneConfigs = append(plan.cloneConfigs, config)
			mirrorConfig, safe := remoteMirrorConfig(config)
			if !safe {
				return plan, false, nil
			}
			if mirrorConfig {
				plan.mirrorConfigs = append(plan.mirrorConfigs, config)
			}
		case strings.HasPrefix(flag, "--config="):
			config, ok := parseCloneConfig(strings.TrimPrefix(flag, "--config="))
			if !ok {
				return plan, false, nil
			}
			plan.cloneConfigs = append(plan.cloneConfigs, config)
			mirrorConfig, safe := remoteMirrorConfig(config)
			if !safe {
				return plan, false, nil
			}
			if mirrorConfig {
				plan.mirrorConfigs = append(plan.mirrorConfigs, config)
			}
		default:
			return plan, false, nil
		}
	}

	for _, option := range cloneFetchOptions {
		if !hasGitOption(fetchParts, option[0]) {
			fetchParts = append(fetchParts, option...)
		}
	}
	for _, part := range fetchParts {
		if isGitFilterFlag(part) {
			plan.partialClone = true
			break
		}
	}
	if !hasGitOption(fetchParts, "--no-tags") {
		// The mirror is an immutable-object source, not an authority for
		// mutable refs. Explicit --tags is rejected by the safety check.
		fetchParts = append(fetchParts, "--no-tags")
	}
	plan.fetchFlags = quoteGitArgs(fetchParts)
	return plan, true, nil
}

// parseCloneConfig parses a git clone --config key=value argument, reporting
// false when the mirror path cannot reproduce clone's semantics for that key.
func parseCloneConfig(arg string) ([2]string, bool) {
	key, value, ok := strings.Cut(arg, "=")
	if !ok || key == "" || configuresOriginRemote(key) || configuresCloneOwnedBranch(key) {
		return [2]string{}, false
	}
	return [2]string{key, value}, true
}

// configuresCloneOwnedBranch reports branch config that git clone derives from
// the cloned repository and checked-out branch. Applying it after acquisition
// would preserve values that a canonical clone overwrites.
func configuresCloneOwnedBranch(key string) bool {
	section, rest, ok := strings.Cut(key, ".")
	if !ok || !strings.EqualFold(section, "branch") {
		return false
	}
	_, _, ok = strings.Cut(rest, ".")
	return ok
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

// remoteMirrorConfig reports whether config should be supplied to the mirror
// command and whether it is safe to use the mirror at all. Most clone config is
// canonical-only and is persisted after acquisition.
func remoteMirrorConfig(config [2]string) (mirror, safe bool) {
	switch {
	case strings.EqualFold(config[0], "fetch.uriProtocols"):
		protocols := strings.Split(config[1], ",")
		if len(protocols) == 0 {
			return false, false
		}
		for _, protocol := range protocols {
			if !strings.EqualFold(strings.TrimSpace(protocol), "https") {
				return false, false
			}
		}
		return true, true
	case strings.EqualFold(config[0], "protocol.version"):
		if config[1] != "2" {
			return false, false
		}
		return true, true
	default:
		return false, true
	}
}

func remoteMirrorFetchFlagsAreSafe(parts []string) bool {
	for i := 0; i < len(parts); i++ {
		part := parts[i]
		switch {
		case isSafeFetchFlagWithoutValue(part):
			continue
		case isGitFilterFlag(part), isShallowFetchFlag(part):
			_, consumed, ok := gitOptionWithValue(parts, i, func(candidate string) bool {
				return isGitFilterFlag(candidate) || isShallowFetchFlag(candidate)
			})
			if !ok {
				return false
			}
			i += consumed
		default:
			return false
		}
	}
	return true
}

func isSafeFetchFlagWithoutValue(flag string) bool {
	switch flag {
	case "-v", "--verbose", "-q", "--quiet",
		"-f", "--force", "--prune", "--no-prune",
		"--no-tags", "--keep", "--progress", "--no-progress",
		"--write-fetch-head", "--no-write-fetch-head", "--update-shallow",
		"--unshallow", "--refetch", "--show-forced-updates",
		"--no-show-forced-updates", "--ipv4", "--ipv6":
		return true
	default:
		return false
	}
}

func isShallowCloneFlag(flag string) bool {
	return gitLongOption(flag, "--depth") ||
		gitLongOption(flag, "--shallow-since") ||
		gitLongOption(flag, "--shallow-exclude")
}

func isShallowFetchFlag(flag string) bool {
	return isShallowCloneFlag(flag) || gitLongOption(flag, "--deepen")
}

func gitLongOption(flag, name string) bool {
	return flag == name || strings.HasPrefix(flag, name+"=")
}

func gitOptionWithValue(parts []string, i int, matches func(string) bool) ([]string, int, bool) {
	if !matches(parts[i]) {
		return nil, 0, false
	}
	if strings.Contains(parts[i], "=") {
		return []string{parts[i]}, 0, true
	}
	if i+1 >= len(parts) {
		return nil, 0, false
	}
	return []string{parts[i], parts[i+1]}, 1, true
}

func hasGitOption(parts []string, candidate string) bool {
	switch {
	case candidate == "--no-tags":
		for _, part := range parts {
			if part == candidate {
				return true
			}
		}
	case isGitFilterFlag(candidate):
		for _, part := range parts {
			if isGitFilterFlag(part) {
				return true
			}
		}
	case isShallowCloneFlag(candidate):
		name := strings.SplitN(candidate, "=", 2)[0]
		for _, part := range parts {
			if gitLongOption(part, name) {
				return true
			}
		}
	}
	return false
}

func quoteGitArgs(parts []string) string {
	quoted := make([]string, len(parts))
	for i, part := range parts {
		quoted[i] = shellwords.Quote(part)
	}
	return strings.Join(quoted, " ")
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
