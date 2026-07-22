package job

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/buildkite/agent/v3/internal/shell"
	"github.com/buildkite/roko"
	"github.com/buildkite/shellwords"
)

// ErrCommitVerificationFailed indicates that git has definitively determined
// the commit is not on the specified branch. This is the security-relevant case.
var ErrCommitVerificationFailed = errors.New("commit verification failed")

// ErrCommitVerificationUnavailable indicates that git was unable to perform
// the ancestry check (e.g. due to repo corruption, misconfigured remotes, etc).
// This is NOT evidence of an attack — it's an infrastructure problem.
var ErrCommitVerificationUnavailable = errors.New("commit verification unavailable")

// checkCommitOnBranch performs the actual git ancestry check, handling shallow
// clones by deepening or unshallowing as needed. It returns:
//   - nil if the commit is verified on the branch
//   - ErrCommitVerificationFailed if the commit is definitively not on the branch
//   - ErrCommitVerificationUnavailable if the check cannot be performed
func (e *Executor) checkCommitOnBranch(ctx context.Context) error {
	e.shell.Commentf("Verifying commit %q is on branch %q", e.Commit, e.Branch)

	// The build's branch usually isn't a local ref at this point: the checkout
	// fetches the commit directly (detached HEAD) and only the repository's
	// default branch gets a local ref from the clone. Fetch the branch tip so
	// there's a resolvable ref to check the commit's ancestry against, and pin it
	// to a SHA (the deepening fetches below overwrite FETCH_HEAD). Without this,
	// merge-base against the bare branch name fails to resolve for any non-default
	// branch and the check silently degrades to "unavailable".
	//
	// Qualify the ref as refs/heads/<branch>: git resolves a bare name against
	// refs/tags/ before refs/heads/, so a tag sharing the branch's name would pin
	// FETCH_HEAD to the tag tip and verify the commit against the tag instead of
	// the branch, letting a commit reachable only from the tag pass.
	//
	// Build the fetch argument vector directly rather than going through gitFetch:
	// gitFetch runs shellwords.Split on every refspec, which mangles a branch name
	// containing shell metacharacters (quotes are legal in git refs). e.Branch is
	// externally controlled, so a split ref could target the wrong branch or fail
	// to parse, silently degrading verification to "unavailable" (warn, never
	// blocking) and defeating the qualification above.
	//
	// Preserve the operator's configured git-fetch flags: they are the supported
	// way to configure every fetch (e.g. --upload-pack for a server on a custom
	// path), and the checkout's own clone and source fetch already honour them. A
	// fetch here that dropped them could fail where those succeed, again degrading
	// to "unavailable" and letting strict pass. Split the flags (they are meant to
	// be word-split) but keep branchRef a single unsplit argument.
	//
	// The deepening fetches below take a stripped copy: git rejects --depth (and
	// --shallow-since/--shallow-exclude) combined with --deepen or --unshallow, so
	// carrying a configured --depth=1 into them makes the deepen fetch exit
	// non-zero and degrades a genuinely off-branch commit on a shallow clone back
	// to "unavailable" (warn, never blocking). Keep the transport flags there but
	// drop the depth-limiting ones.
	branchRef := "refs/heads/" + e.Branch
	fetchFlags, err := shellwords.Split(e.GitFetchFlags)
	if err != nil {
		return fmt.Errorf("%w: unable to parse git-fetch-flags %q: %w", ErrCommitVerificationUnavailable, e.GitFetchFlags, err)
	}
	deepenFlags := stripShallowFetchFlags(fetchFlags)
	fetchBranch := func(baseFlags []string, extraFlags ...string) error {
		args := append([]string{"fetch"}, baseFlags...)
		args = append(args, extraFlags...)
		args = append(args, "--", "origin", branchRef)
		return e.shell.Command("git", args...).Run(ctx)
	}

	// Retry the branch-tip fetch a few times: it is the one network dependency the
	// check adds, and a transient failure would otherwise degrade the whole check
	// to "unavailable" (warn, never blocking) with no second chance, since
	// verifyCommit returns nil and the outer checkout retry loop won't re-run it.
	fetchErr := roko.NewRetrier(
		roko.WithMaxAttempts(3),
		roko.WithStrategy(roko.ExponentialSubsecond(time.Second)),
		roko.WithJitter(),
	).DoWithContext(ctx, func(*roko.Retrier) error {
		return fetchBranch(fetchFlags)
	})
	if fetchErr != nil {
		return fmt.Errorf("%w: unable to fetch branch %q: %w", ErrCommitVerificationUnavailable, e.Branch, fetchErr)
	}
	branchTip, err := e.shell.Command("git", "rev-parse", "FETCH_HEAD").RunAndCaptureStdout(ctx)
	if err != nil {
		return fmt.Errorf("%w: unable to resolve branch %q: %w", ErrCommitVerificationUnavailable, e.Branch, err)
	}
	branchTip = strings.TrimSpace(branchTip)

	// merge-base --is-ancestor walks back from the branch tip: exit 0 means the
	// commit is reachable from it (definitive, even on a shallow clone), exit 1
	// means it is not. On a shallow clone a "not an ancestor" result can be a
	// false negative when the connecting history lies beyond the shallow boundary,
	// so a negative (or otherwise inconclusive) result is only trusted once the
	// repository is no longer shallow; until then we deepen the branch and re-check.
	for _, fetchFlag := range []string{"", "--deepen=50", "--unshallow"} {
		if fetchFlag != "" {
			e.shell.Commentf("Deepening checkout to verify commit (%s)...", fetchFlag)
			if fetchErr := fetchBranch(deepenFlags, fetchFlag); fetchErr != nil {
				return fmt.Errorf("%w: unable to verify commit %q on branch %q: %w", ErrCommitVerificationUnavailable, e.Commit, e.Branch, fetchErr)
			}
		}

		mergeBaseErr := e.shell.Command("git", "merge-base", "--is-ancestor", e.Commit, branchTip).Run(ctx)
		if mergeBaseErr == nil {
			return nil // commit is reachable from the branch tip: verified
		}
		// A non-exit error (e.g. git failed to spawn) tells us nothing about
		// ancestry, so treat it as unavailable rather than a definitive failure.
		if !shell.IsExitError(mergeBaseErr) {
			return fmt.Errorf("%w: unable to verify commit %q on branch %q: %w", ErrCommitVerificationUnavailable, e.Commit, e.Branch, mergeBaseErr)
		}
		exitCode := shell.ExitCode(mergeBaseErr)

		shallow, shallowErr := e.shell.Command("git", "rev-parse", "--is-shallow-repository").RunAndCaptureStdout(ctx)
		if shallowErr != nil {
			return fmt.Errorf("%w: unable to verify commit %q on branch %q: %w", ErrCommitVerificationUnavailable, e.Commit, e.Branch, shallowErr)
		}
		if strings.TrimSpace(shallow) != "true" {
			// Full history, so the result is now definitive.
			if exitCode == 1 {
				return fmt.Errorf("%w: commit %q is not on branch %q", ErrCommitVerificationFailed, e.Commit, e.Branch)
			}
			return fmt.Errorf("%w: unable to verify commit %q on branch %q", ErrCommitVerificationUnavailable, e.Commit, e.Branch)
		}
		// Still shallow, so deepen on the next iteration and re-check.
	}

	// All attempts exhausted, so verification is unavailable.
	return fmt.Errorf("%w: unable to verify commit %q on branch %q after exhausting fetch strategies", ErrCommitVerificationUnavailable, e.Commit, e.Branch)
}

// stripShallowFetchFlags removes depth-limiting options from a git-fetch flag
// list. git treats --depth, --deepen, --shallow-since and --shallow-exclude as
// mutually exclusive with the --deepen/--unshallow fetches checkCommitOnBranch
// issues, so those fetches must not inherit them from BUILDKITE_GIT_FETCH_FLAGS.
// Both the "--flag=value" and "--flag value" spellings are handled; the latter
// also drops the following value token. --unshallow is dropped too, since we add
// it ourselves.
func stripShallowFetchFlags(flags []string) []string {
	valueFlags := map[string]bool{
		"--depth":           true,
		"--deepen":          true,
		"--shallow-since":   true,
		"--shallow-exclude": true,
	}
	out := make([]string, 0, len(flags))
	for i := 0; i < len(flags); i++ {
		name, _, hasValue := strings.Cut(flags[i], "=")
		if name == "--unshallow" {
			continue
		}
		if valueFlags[name] {
			if !hasValue {
				i++ // skip the separate value token in the "--flag value" form
			}
			continue
		}
		out = append(out, flags[i])
	}
	return out
}

// verifyCommit is called if the user has commit verification enabled. It ensures that the commit we are
// asked to build exists and is reachable on the branch we are given.
func (e *Executor) verifyCommit(ctx context.Context) error {
	// Skip if not enabled
	if e.GitCommitVerification == "" {
		return nil
	}

	// Skip if commit is HEAD (nothing to verify)
	if e.Commit == "HEAD" {
		e.shell.Commentf("Skipping commit verification: commit is HEAD")
		return nil
	}

	// Skip if we haven't been given a branch - e.g. it's a tag push event
	if e.Branch == "" {
		e.shell.Commentf("Skipping commit verification: no branch specified")
		return nil
	}

	// Skip if this is a tag build — tags are not branch-specific
	if e.Tag != "" {
		e.shell.Commentf("Skipping commit verification: tag build (%s)", e.Tag)
		return nil
	}

	// Skip if this is a PR build: the commit may be on a merge ref, not the target
	// branch. BUILDKITE_PULL_REQUEST is the string "false" (not empty) on non-PR builds.
	if e.PullRequest != "" && e.PullRequest != "false" {
		e.shell.Commentf("Skipping commit verification: pull request build (#%s)", e.PullRequest)
		return nil
	}

	// Skip if a custom refspec is set — the fetch may not populate standard branch refs,
	// making ancestry verification unreliable
	if e.RefSpec != "" {
		e.shell.Commentf("Skipping commit verification: custom refspec is set (%s)", e.RefSpec)
		return nil
	}

	// Perform the verification
	err := e.checkCommitOnBranch(ctx)

	// Verification passed
	if err == nil {
		return nil
	}

	// Definitive failure — commit is provably not on the branch
	if errors.Is(err, ErrCommitVerificationFailed) {
		if e.GitCommitVerification == "strict" {
			return err
		}
		// err already begins with "commit verification failed", so log it as-is.
		e.shell.Warningf("%s", err)
		return nil
	}

	// Verification unavailable — infrastructure issue, not a security concern.
	// We always warn but never block, even in strict mode, to avoid users
	// disabling verification entirely due to infrastructure false positives.
	// err already begins with "commit verification unavailable", so log it as-is.
	e.shell.Warningf("%s", err)
	return nil
}
