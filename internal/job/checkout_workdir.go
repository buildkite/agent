package job

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/buildkite/agent/v4/internal/osutil"
	"github.com/buildkite/agent/v4/internal/shell"
	"github.com/buildkite/agent/v4/tracetools"
	"go.opentelemetry.io/otel/attribute"
)

// prepareCheckoutWorkdir reconciles an existing checkout or clones a fresh
// one. It intentionally owns the complete existing-vs-fresh decision so
// checkout.go remains orchestration rather than accumulating another
// cross-cutting branch. It reports whether the fresh checkout was derived from
// the mirror snapshot, in which case the objects for this build's target are
// already local and the usual fetch can be skipped.
func (e *Executor) prepareCheckoutWorkdir(
	ctx context.Context,
	attempt *remoteMirrorAttempt,
	sparse sparseCheckout,
	mirror mirrorReference,
	gitCloneFlags []string,
	userSuppliedCloneFilter bool,
) (derivedFromSnapshot bool, retErr error) {
	// On mirrors, snapshots and dissociation:
	//
	// --reference makes the clone reuse objects from the mirror, using the
	// .git/objects/info/alternates file. On its own, it won't copy the objects
	// from the mirror, just refer to them. This becomes a problem if they
	// disappear, which happens during routine normal use of the mirror.
	//
	// --dissociate makes copies of the objects from the mirror, which makes the
	// clone robust against that failure, at the expense of disk space and extra
	// work up front.
	//
	// A per-job mirror snapshot (see snapshotMirror) removes the fragility a
	// different way: the snapshot is immutable and outlives the checkout's use
	// of it, so referencing it is safe without dissociating. It is also a
	// complete local copy of the mirror, so when one exists, the checkout is
	// cloned *from* the snapshot rather than from the canonical repository,
	// eliminating the clone's network round-trip entirely.

	existingGitDir := filepath.Join(e.shell.Getwd(), ".git")
	if osutil.FileExists(existingGitDir) {
		if _, err := e.updateRemoteURL(ctx, "", e.Repository); err != nil {
			return false, fmt.Errorf("setting origin: %w", err)
		}

		if mirror.dir == "" && e.GitMirrorsPath != "" {
			// A bypassed mirror must not remain reachable through a stale alternate.
			if err := e.traceOp(ctx, "git.dissociate", func(ctx context.Context) error {
				return e.dissociateIfNeeded(ctx, existingGitDir)
			}); err != nil {
				return false, fmt.Errorf("dissociating bypassed mirror: %w", err)
			}
		} else if mirror.dir != "" {
			switch e.GitMirrorCheckoutMode {
			case "dissociate":
				if err := e.traceOp(ctx, "git.dissociate", func(ctx context.Context) error {
					return e.dissociateIfNeeded(ctx, existingGitDir)
				}); err != nil {
					return false, fmt.Errorf("dissociating existing reference clone: %w", err)
				}
			case "reference":
				if err := e.reassociateIfNeeded(ctx, existingGitDir, mirror.dir); err != nil {
					return false, fmt.Errorf("reassociating existing clone: %w", err)
				}
			}
		}
		return false, nil
	}

	deriveFromSnapshot := mirror.isSnapshot && cloneFlagsAllowSnapshotDerive(gitCloneFlags)

	if mirror.dir != "" && !deriveFromSnapshot {
		gitCloneFlags = append(gitCloneFlags, "--reference", mirror.dir)
		if e.GitMirrorCheckoutMode == "dissociate" {
			gitCloneFlags = append(gitCloneFlags, "--dissociate")
		}
	}

	if sparse.active() {
		if slices.Contains(gitCloneFlags, "--sparse") {
			e.shell.Commentf("Sparse checkout is configured and BUILDKITE_GIT_CLONE_FLAGS already contains a --sparse flag (preserving user-supplied sparse checkout).")
		} else {
			gitCloneFlags = append(gitCloneFlags, "--sparse")
		}
		if userSuppliedCloneFilter {
			e.shell.Commentf("Sparse checkout is configured and BUILDKITE_GIT_CLONE_FLAGS already contains a --filter (preserving user-supplied filter).")
		} else {
			gitCloneFlags = append(gitCloneFlags, "--filter=blob:none")
		}
	}

	cloneSpan, cloneCtx := e.traceOpSpan(ctx, "git.clone")
	mirrorMode := "none"
	if mirror.dir != "" {
		mirrorMode = e.GitMirrorCheckoutMode
	}
	cloneSpan.SetAttributes(
		attribute.String("git.mirror_mode", mirrorMode),
		attribute.String("git.clone_source", "canonical"),
		attribute.Bool("git.sparse", sparse.active()),
		attribute.Bool("git.blobless_filter", hasPartialFilterFlags(gitCloneFlags)),
	)
	defer func() { tracetools.FinishWithError(cloneSpan, retErr) }()

	if deriveFromSnapshot {
		derived, err := e.deriveCheckoutFromSnapshot(cloneCtx, mirror.dir, gitCloneFlags)
		if err != nil {
			return false, err
		}
		if derived {
			cloneSpan.SetAttributes(attribute.String("git.clone_source", "snapshot"))
			return true, nil
		}
		// Fall through to a clean clone from the canonical repository. The
		// snapshot (or the mirror behind it) may be broken, so don't reference
		// it: the mirror layer is an optimization, never a correctness
		// dependency.
	}

	cloneCheckout := func(
		gitFlags []string,
		repository string,
		runOpts ...shell.RunCommandOpt,
	) error {
		return gitClone(cloneCtx, e.shell, gitFlags, gitCloneFlags, repository, ".", runOpts...)
	}
	if isFreshCloneRemoteMirrorAttempt(attempt) {
		started := time.Now()
		// C3: a mirror without filter support silently performs a full
		// transfer. Preserve Git's clone semantics and expose the cost through
		// telemetry rather than reconstructing clone capability negotiation.
		cloneErr := cloneCheckout(
			remoteMirrorBulkGitFlags(ctx),
			attempt.url,
			shell.AlwaysHidePrompt(),
		)
		if cloneErr == nil {
			// C1/C14: refs initially reflect the mirror, but durable origin is
			// canonical and a killed process self-heals on the next checkout.
			cloneErr = e.shell.Command(
				"git", "remote", "set-url", "origin", e.Repository,
			).Run(ctx)
		}
		shallowClone := false
		if cloneErr == nil {
			shallow, err := e.shell.Command(
				"git", "rev-parse", "--is-shallow-repository",
			).RunAndCaptureStdout(ctx)
			if err != nil {
				cloneErr = fmt.Errorf("determining whether mirror clone is shallow: %w", err)
			} else {
				shallowClone = strings.TrimSpace(shallow) == "true"
			}
		}
		hasCommit := false
		if cloneErr == nil {
			hasCommit = hasGitCommit(ctx, e.shell, ".git", e.Commit)
		}
		attempt.duration = time.Since(started)
		if ctxErr := ctx.Err(); ctxErr != nil {
			if cleanupErr := e.removeCheckoutDir(); cleanupErr != nil {
				return false, errors.Join(ctxErr, cleanupErr)
			}
			return false, ctxErr
		}
		if cloneErr == nil {
			ambientAlternates, _ := e.shell.Env.Get("GIT_ALTERNATE_OBJECT_DIRECTORIES")
			alternateObjects := ambientAlternates != "" ||
				osutil.FileExists(filepath.Join(e.shell.Getwd(), ".git", "objects", "info", "alternates"))
			if hasCommit && (!shallowClone || !alternateObjects) {
				// C1: a shallow hit keeps the mirror snapshot's boundary even
				// if canonical has advanced. Re-cloning every hit would discard
				// the optimization for the shallow workloads it targets.
				attempt.outcome = remoteMirrorOutcomeHit
				cloneSpan.SetAttributes(attribute.String("git.clone_source", "remote-mirror"))
				return false, nil
			}
			attempt.outcome = remoteMirrorOutcomeMiss
			if !shallowClone {
				// Keep the useful mirror clone. fetchSource will fetch only
				// the missing immutable commit from canonical.
				cloneSpan.SetAttributes(attribute.String("git.clone_source", "remote-mirror"))
				return false, nil
			}

			// A canonical fetch into a lagging shallow clone keeps the
			// mirror's old shallow boundary and can expose extra history.
			// Alternates can also make a missing mirror commit look present.
			// Re-clone so canonical owns the shallow boundary in both cases.
			e.shell.Commentf("Remote Git mirror is lagging for a shallow clone; cloning from canonical repository")
			if cleanupErr := e.removeCheckoutDir(); cleanupErr != nil {
				return false, cleanupErr
			}
			if err := e.createCheckoutDir(); err != nil {
				return false, err
			}
		} else {
			var gitErr *gitError
			if errors.As(cloneErr, &gitErr) && gitErr.Type == gitErrorCloneTimeout {
				attempt.outcome = remoteMirrorOutcomeTimeout
			} else {
				attempt.outcome = remoteMirrorOutcomeError
			}
			if cleanupErr := e.removeCheckoutDir(); cleanupErr != nil {
				return false, errors.Join(cloneErr, cleanupErr)
			}
			if err := e.createCheckoutDir(); err != nil {
				return false, errors.Join(cloneErr, err)
			}
			e.shell.Commentf("Remote Git mirror clone failed; falling back to canonical repository")
		}
	}

	if err := cloneCheckout(nil, e.Repository); err != nil {
		return false, fmt.Errorf("cloning git repository: %w", err)
	}
	return false, nil
}

func isFreshCloneRemoteMirrorAttempt(attempt *remoteMirrorAttempt) bool {
	return attempt != nil &&
		attempt.site == remoteMirrorSiteFreshClone &&
		attempt.outcome == remoteMirrorOutcomeNotReached
}

// deriveCheckoutFromSnapshot clones the checkout from the job's immutable
// mirror snapshot, without contacting the canonical repository. The snapshot
// is only the transport: origin is rewritten to the canonical repository
// immediately afterwards, so later fetches (including partial-clone lazy
// fetches) and job code see the real remote.
//
// The local clone succeeding does not by itself prove the build's target is
// usable — the later checkout of the target commit is the real validation. On
// failure the partial checkout is removed and (false, nil) is returned so the
// caller falls back to cloning from canonical: the mirror layer is an
// optimization, never a correctness dependency.
func (e *Executor) deriveCheckoutFromSnapshot(ctx context.Context, snapshotDir string, gitCloneFlags []string) (derived bool, retErr error) {
	e.shell.Commentf("Cloning from mirror snapshot %q", snapshotDir)

	flags := slices.Clone(gitCloneFlags)
	// These flags are mandatory, and deliberately appended after any
	// user-supplied flags so they cannot be overridden:
	//
	// --no-local: cloning from a local path otherwise uses git's local
	// hardlink/copy optimization *regardless of --reference*, which would tie
	// the checkout's object store to the snapshot's files instead of the
	// alternates mechanism the snapshot lease is built on.
	//
	// --no-checkout: the working tree is written exactly once, by the later
	// git checkout of the build's target commit, instead of first
	// materializing the snapshot's HEAD.
	flags = append(flags, "--no-checkout", "--no-local", "--reference", snapshotDir)
	if e.GitMirrorCheckoutMode == "dissociate" {
		flags = append(flags, "--dissociate")
	}

	err := gitClone(ctx, e.shell, nil, flags, snapshotDir, ".")
	if err == nil {
		err = e.shell.Command("git", "remote", "set-url", "origin", e.Repository).Run(ctx)
	}
	if err == nil {
		e.syncOriginBranchRef(ctx)
		return true, nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return false, ctxErr
	}
	e.shell.Warningf("Clone from mirror snapshot failed (%v); falling back to canonical repository", err)
	if rmErr := e.removeCheckoutDir(); rmErr != nil {
		return false, errors.Join(err, rmErr)
	}
	if mkErr := e.createCheckoutDir(); mkErr != nil {
		return false, errors.Join(err, mkErr)
	}
	return false, nil
}

// syncOriginBranchRef points refs/remotes/origin/<branch> at the build's
// commit when the snapshot's copy is stale or missing. A clone from the
// canonical repository sees that remote's current refs, but a clone from a
// mirror snapshot sees whatever the mirror last fetched, and the mirror only
// guarantees the build's own target. The build branch is the remote-tracking
// ref job code most commonly reads (e.g. to diff against), so keep it at
// least as fresh as the build's own commit. Staleness of other remote refs is
// accepted, documented behavior for snapshot-derived clones.
//
// This is best-effort: it only applies to plain branch builds (for a
// pull-request build the head may not exist on origin at all, and tag or
// custom-refspec builds don't map to a branch ref), and failures only warn.
func (e *Executor) syncOriginBranchRef(ctx context.Context) {
	if e.Commit == "HEAD" || e.Branch == "" || e.RefSpec != "" || e.Tag != "" || e.PullRequest != "false" {
		return
	}
	ref := "refs/remotes/origin/" + e.Branch
	if !gitCheckRefFormat(ref) || !hasGitCommit(ctx, e.shell, ".git", e.Commit) {
		return
	}
	// Leave the ref alone when it already contains the commit: it is as new
	// or newer, e.g. a rebuild of an older commit.
	if err := e.shell.Command("git", "merge-base", "--is-ancestor", e.Commit, ref).Run(ctx, shell.ShowStderr(false)); err == nil {
		return
	}
	if err := e.shell.Command("git", "update-ref", ref, e.Commit).Run(ctx); err != nil {
		e.shell.Warningf("Could not update %s to %s: %v", ref, e.Commit, err)
	}
}

// snapshotDeriveIncompatibleCloneFlags are clone flag prefixes that make a
// fresh checkout ineligible for cloning from the local mirror snapshot,
// because they select transport or remote-shape semantics that only the
// canonical repository provides, or they conflict with the flags
// deriveCheckoutFromSnapshot appends. Matching is by conservative prefix,
// since git accepts unambiguous long-option abbreviations; an over-match only
// means falling back to today's canonical --reference clone.
var snapshotDeriveIncompatibleCloneFlags = []string{
	"-b", "--br", // --branch: selects a branch from the canonical remote
	"--dep",      // --depth: the shallow boundary must be canonical's
	"--sha",      // --shallow-since / --shallow-exclude
	"--si",       // --single-branch: depends on the remote's HEAD
	"-o", "--or", // --origin: the derive rewrites the remote named origin
	"-l", "--lo", // --local: the exact mode --no-local must suppress
	"--mi", // --mirror
	"--ba", // --bare
	"--bu", // --bundle-uri: bootstraps objects from elsewhere
}

// cloneFlagsAllowSnapshotDerive reports whether the user-supplied clone flags
// are compatible with deriving the fresh checkout from the mirror snapshot.
func cloneFlagsAllowSnapshotDerive(flags []string) bool {
	// Clone-time recursive submodules resolve URLs and clone while origin
	// still points at the snapshot, and an external git dir would survive
	// checkout-directory cleanup on fallback.
	if hasRecursiveSubmoduleCloneFlags(flags) || hasSeparateGitDirCloneFlag(flags) {
		return false
	}
	for _, flag := range flags {
		for _, prefix := range snapshotDeriveIncompatibleCloneFlags {
			if strings.HasPrefix(flag, prefix) {
				return false
			}
		}
	}
	return true
}
