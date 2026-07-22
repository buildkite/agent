package job

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/buildkite/agent/v4/internal/shell"
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

	for _, fetchFlag := range []string{"", "--deepen=50", "--unshallow"} {
		if fetchFlag != "" {
			// After the first iteration, try to unshallow the repo (a bit or a lot)
			e.shell.Commentf("Deepening checkout to verify commit (%s)...", fetchFlag)
			fetchErr := e.shell.Command("git", "fetch", fetchFlag).Run(ctx)
			if fetchErr != nil {
				return fmt.Errorf("%w: unable to verify commit %q on branch %q: %w", ErrCommitVerificationUnavailable, e.Commit, e.Branch, fetchErr)
			}
		}

		// Try the ancestry check
		err := e.shell.Command("git", "merge-base", "--is-ancestor", e.Commit, e.Branch).Run(ctx)

		switch shell.ExitCode(err) {
		case 0:
			return nil // verified!
		case 1:
			return fmt.Errorf("%w: commit %q is not on branch %q", ErrCommitVerificationFailed, e.Commit, e.Branch)
		case 128:
			// unclear — continue below
		default:
			return fmt.Errorf("%w: unable to verify commit %q on branch %q: %w", ErrCommitVerificationUnavailable, e.Commit, e.Branch, err)
		}

		// On the first iteration, check if the checkout is shallow. If it is
		// not, the 128 exit code reflects some other error.
		if fetchFlag != "" {
			continue
		}
		output, shallowErr := e.shell.Command("git", "rev-parse", "--is-shallow-repository").RunAndCaptureStdout(ctx)
		if shallowErr != nil {
			return fmt.Errorf("%w: unable to verify commit %q on branch %q: %w", ErrCommitVerificationUnavailable, e.Commit, e.Branch, shallowErr)
		}

		if strings.TrimSpace(output) != "true" {
			// Not shallow — this is a genuine error
			return fmt.Errorf("%w: unable to verify commit %q on branch %q: %w", ErrCommitVerificationUnavailable, e.Commit, e.Branch, err)
		}
	}

	// All attempts exhausted — verification is unavailable
	return fmt.Errorf("%w: unable to verify commit %q on branch %q after exhausting fetch strategies", ErrCommitVerificationUnavailable, e.Commit, e.Branch)
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

	// Skip if this is a PR build — the commit may be on a merge ref, not the target branch
	if e.PullRequest != "" {
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
		e.shell.Warningf("Commit verification failed: %v", err)
		return nil
	}

	// Verification unavailable — infrastructure issue, not a security concern.
	// We always warn but never block, even in strict mode, to avoid users
	// disabling verification entirely due to infrastructure false positives.
	e.shell.Warningf("Commit verification unavailable: %v", err)
	return nil
}
