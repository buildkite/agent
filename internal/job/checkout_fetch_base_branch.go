package job

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/buildkite/roko"
)

// How hard the checkout tries to bring refs/remotes/origin/<base> up to date.
// The base branch is the ref a `git diff` later in the job resolves, and the
// job's own fetch never updates it, so a reused checkout directory holds
// whatever an earlier build left there.
const (
	// GitFetchBaseBranchOff does not fetch the base branch at all.
	GitFetchBaseBranchOff = "off"

	// GitFetchBaseBranchOptimistic fetches the base branch and warns, without
	// failing the job, when it cannot: the checkout is complete without it, and a
	// stale base only ever widens a diff.
	GitFetchBaseBranchOptimistic = "optimistic"

	// GitFetchBaseBranchStrict fails the checkout when the base branch cannot be
	// fetched, for jobs whose correctness depends on the diff being right rather
	// than merely wide. Freshness cannot be guaranteed without failing closed.
	GitFetchBaseBranchStrict = "strict"
)

// Retry budgets for the base branch fetch, in attempts of the subsecond
// exponential backoff below. An outage at the remote is the usual reason a fetch
// fails in CI, so both modes ride out a blip rather than leaving a stale ref
// behind — under optimistic that ref would be a silently wider diff, reported as
// nothing but a warning.
//
// They differ in how long that is worth. strict gets the same count as the job's
// own fetch, about a minute and a half of waiting, because the alternative is
// failing the job. optimistic has already decided the job proceeds either way, so
// it spends a couple of seconds covering the blip it can and then gets out of the
// way.
const (
	baseBranchFetchAttemptsOptimistic = 3
	baseBranchFetchAttemptsStrict     = 10
)

// errNoBaseBranchToFetch is the strict-mode failure for a job that names no base
// branch at all. Retrying the checkout cannot conjure one, so the checkout breaks
// its retrier on it rather than spending the whole attempt budget on it.
var errNoBaseBranchToFetch = errors.New("no base branch to fetch")

// parseGitFetchBaseBranchMode maps a BUILDKITE_GIT_FETCH_BASE_BRANCH value to a
// mode. The empty string selects off, for programmatic ExecutorConfig consumers;
// the CLI defaults the flag to off and rejects anything outside the three modes,
// but the value can also arrive from job env.
func parseGitFetchBaseBranchMode(s string) (string, error) {
	switch s {
	case "", GitFetchBaseBranchOff:
		return GitFetchBaseBranchOff, nil
	case GitFetchBaseBranchOptimistic, GitFetchBaseBranchStrict:
		return s, nil
	default:
		return "", fmt.Errorf(
			"invalid git fetch base branch mode %q (must be %q, %q or %q)",
			s,
			GitFetchBaseBranchOff,
			GitFetchBaseBranchOptimistic,
			GitFetchBaseBranchStrict,
		)
	}
}

// baseBranchToFetch returns the branch a diff in this job would be taken against.
// The second return distinguishes the two ways there can be nothing to fetch: the
// job is building the base branch itself, or nothing names a base branch at all —
// which strict mode treats as a failure, since a job that diffs anyway would be
// reading a ref this checkout never touched.
//
// The precedence is the one `pipeline upload` already uses for --git-diff-base,
// minus its final literal "main": guessing a name would mean a failed fetch on
// every repository whose default branch is called something else.
func (e *Executor) baseBranchToFetch() (base string, buildingBase bool) {
	built := strings.TrimPrefix(e.Branch, "refs/heads/")
	for _, name := range []string{"BUILDKITE_PULL_REQUEST_BASE_BRANCH", "BUILDKITE_PIPELINE_DEFAULT_BRANCH"} {
		branch, _ := e.shell.Env.Get(name)
		branch = strings.TrimPrefix(branch, "refs/heads/")
		if branch == "" {
			continue
		}
		if branch == built {
			// There is no distinct base branch to prepare when building the base itself.
			return "", true
		}
		return branch, false
	}
	return "", false
}

// fetchBaseBranch updates refs/remotes/origin/<base>, which the job's own fetch
// does not: it asks for refs/pull/N/head and the commit, and checkout directories
// are reused, so that ref otherwise holds whatever an earlier build left there.
//
// It is separate from that fetch because FETCH_HEAD resolves to the first ref
// fetched and the checkout depends on it. The refspec is explicit and forced so the
// remote-tracking ref lands whatever remote.origin.fetch says, and follows a
// force-pushed base branch instead of stopping at a non-fast-forward rejection.
//
// The mode is the one fetchSource parsed, so an invalid value has already failed
// the checkout by the time this runs.
//
// A transient failure is retried in both modes, on the budgets above, and a remote
// that answers "no such ref" ends the retries at once, so a deleted base branch
// costs one fetch rather than the whole budget (see baseBranchFetchIsRetryable).
// Under strict the exhausted error is returned and the checkout's own retrier gets
// a further go at it, classifying it exactly as it does a failure to fetch the
// job's own source.
func (e *Executor) fetchBaseBranch(ctx context.Context, mode, gitFetchFlags string) error {
	if mode == GitFetchBaseBranchOff {
		return nil
	}
	strict := mode == GitFetchBaseBranchStrict

	base, buildingBase := e.baseBranchToFetch()
	switch {
	case buildingBase:
		e.shell.Commentf("Skipping base branch fetch: the branch being built is the base branch")
		return nil

	case base == "":
		if strict {
			return fmt.Errorf(
				"%w: neither BUILDKITE_PULL_REQUEST_BASE_BRANCH nor BUILDKITE_PIPELINE_DEFAULT_BRANCH is set, so BUILDKITE_GIT_FETCH_BASE_BRANCH=%s cannot guarantee a current base branch",
				errNoBaseBranchToFetch, GitFetchBaseBranchStrict,
			)
		}
		e.shell.Commentf("Skipping base branch fetch: no base branch is known")
		return nil
	}

	e.shell.Commentf("Fetch base branch %q", base)

	attempts := baseBranchFetchAttemptsOptimistic
	if strict {
		attempts = baseBranchFetchAttemptsStrict
	}

	refspec := fmt.Sprintf("+refs/heads/%s:refs/remotes/origin/%s", base, base)
	err := roko.NewRetrier(
		roko.WithMaxAttempts(attempts),
		roko.WithStrategy(roko.ExponentialSubsecond(time.Second)),
		roko.WithJitter(),
	).DoWithContext(ctx, func(r *roko.Retrier) error {
		err := gitFetch(ctx, gitFetchArgs{
			Shell:         e.shell,
			GitFetchFlags: gitFetchFlags,
			Repository:    "origin",
			// LiteralRefSpecs, not RefSpecs: a quote is legal in a git ref name, and
			// word-splitting this refspec would turn release'candidate into
			// releasecandidate — a fetch that succeeds against the wrong branch
			// wherever that one exists, leaving the intended ref stale.
			LiteralRefSpecs: []string{refspec},
		})
		if err == nil {
			return nil
		}
		if !baseBranchFetchIsRetryable(err) {
			r.Break()
			return err
		}
		e.shell.Commentf("Couldn't fetch base branch %q (%s)", base, r)
		return err
	})
	if err != nil {
		if !strict {
			e.shell.Warningf("Couldn't fetch base branch %q, continuing: %v", base, err)
			return nil
		}
		return fmt.Errorf("fetching base branch %q: %w", base, err)
	}
	return nil
}

// baseBranchFetchIsRetryable reports whether a failed base branch fetch is worth
// another attempt. A remote that answers "that ref does not exist", or a local
// object store that is corrupt, has given a real answer, and retrying only delays
// it by the whole budget. Everything else — transport errors, and git's broad exit
// 128, which is what an outage at the remote looks like — may well succeed next
// time.
//
// This is also why the fetch cannot just pass Retry to gitFetch: that retrier
// deliberately keeps retrying a missing ref, to wait out GitHub creating a pull
// request's refs/pull/N/head asynchronously. A base branch is either already there
// or gone for good.
func baseBranchFetchIsRetryable(err error) bool {
	var gitErr *gitError
	if !errors.As(err, &gitErr) {
		// gitFetch's only non-gitError failures are flag and refspec parse errors,
		// which the next attempt would hit identically.
		return false
	}
	switch gitErr.Type {
	case gitErrorFetchBadReference, gitErrorFetchRefNotOnRemote, gitErrorFetchBadObject:
		return false
	default:
		return true
	}
}
