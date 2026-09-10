package job

import (
	"fmt"
	"strings"
)

// checkoutTargetKind is the category of git ref a checkout fetches to reach
// its target commit.
type checkoutTargetKind string

const (
	// e.RefSpec is set, overriding all other fetch behaviour
	targetCustomRefspec checkoutTargetKind = "custom"

	// GitHub PR build using the speculative merge ref (refs/pull/N/merge)
	targetGithubPRMerge checkoutTargetKind = "github-pr-merge"

	// GitHub PR build using the PR's head ref (refs/pull/N/head)
	targetGithubPRHead checkoutTargetKind = "github-pr-head"

	// No specific commit is known (BUILDKITE_COMMIT is "HEAD"), so fetch the
	// branch's remote tip and build whatever it points at.
	targetBranch checkoutTargetKind = "branch"

	// Default: a specific commit is known, so fetch and check it out directly.
	targetCommit checkoutTargetKind = "commit"
)

// checkoutTarget is what a job's checkout must end up at, derived once from
// the job's configuration. Every stage of the checkout (on-host mirror update,
// remote mirror eligibility, source fetch, commit verification, and the final
// git checkout) consults it instead of re-deriving the answer from e.Commit,
// e.Branch, e.RefSpec, e.PullRequest and e.PipelineProvider independently.
type checkoutTarget struct {
	kind checkoutTargetKind

	// commit is the commit the build was created for, or "" when the build
	// was created without one (BUILDKITE_COMMIT == "HEAD"). With no commit the
	// checkout resolves one from whatever refspec fetches, so it must never
	// fetch anything else at the same time.
	commit string

	// refspec is the ref fetched from origin to obtain the target: the custom
	// refspec, refs/pull/N/{head,merge}, or the build branch. It may be empty
	// for a commit build with no branch (e.g. a tag push).
	refspec string

	// retryFetch reports whether fetching refspec should be retried when the
	// ref is missing. GitHub creates refs/pull/N/head asynchronously, so a
	// miss there is usually transient; a missing refs/pull/N/merge usually
	// means a real merge conflict and should fail fast.
	retryFetch bool

	// pullRequest is the pull request number when this build is for a pull
	// request from any provider, or "" otherwise. This is broader than the
	// GitHub PR kinds: a Bitbucket or GitLab PR build has pullRequest set but
	// is fetched as a plain branch/commit.
	pullRequest string
}

// checkoutTarget classifies the job's configuration into a checkoutTarget.
func (e *Executor) checkoutTarget() checkoutTarget {
	t := checkoutTarget{refspec: e.Branch}
	if e.Commit != "HEAD" {
		t.commit = e.Commit
	}
	// The backend sends the literal string "false" for non-PR builds; treat an
	// unset value the same way.
	if e.PullRequest != "" && e.PullRequest != "false" {
		t.pullRequest = e.PullRequest
	}

	switch {
	case e.RefSpec != "":
		t.kind = targetCustomRefspec
		t.refspec = e.RefSpec
	case t.pullRequest != "" && strings.Contains(e.PipelineProvider, "github"):
		if e.PullRequestUsingMergeRefspec {
			t.kind = targetGithubPRMerge
			t.refspec = fmt.Sprintf("refs/pull/%s/merge", t.pullRequest)
		} else {
			t.kind = targetGithubPRHead
			t.refspec = fmt.Sprintf("refs/pull/%s/head", t.pullRequest)
			t.retryFetch = true
		}
	case t.commit == "":
		t.kind = targetBranch
	default:
		t.kind = targetCommit
	}
	return t
}

// commitKnown reports whether the build was created for a specific commit.
func (t checkoutTarget) commitKnown() bool { return t.commit != "" }

// checkoutRef is the ref passed to `git checkout` once the target has been
// fetched: the known commit, or FETCH_HEAD when the commit had to be
// resolved from the fetched refspec.
func (t checkoutTarget) checkoutRef() string {
	if t.commitKnown() {
		return t.commit
	}
	return "FETCH_HEAD"
}
