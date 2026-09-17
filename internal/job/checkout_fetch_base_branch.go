package job

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
// Not retried here in either mode: the retry on the job's own fetch waits out
// asynchronous creation of the ref being built, which a deleted base branch never
// gets, so inheriting it would spend ~2m17s of every job discovering that. Under
// strict the failure is returned instead, and the checkout's own retrier covers a
// transient one, classifying it exactly as it does a failure to fetch the job's own
// source.
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

	refspec := fmt.Sprintf("+refs/heads/%s:refs/remotes/origin/%s", base, base)
	if err := gitFetch(ctx, gitFetchArgs{
		Shell:         e.shell,
		GitFetchFlags: gitFetchFlags,
		Repository:    "origin",
		RefSpecs:      []string{refspec},
	}); err != nil {
		if !strict {
			e.shell.Warningf("Couldn't fetch base branch %q, continuing: %v", base, err)
			return nil
		}
		return fmt.Errorf("fetching base branch %q: %w", base, err)
	}
	return nil
}
