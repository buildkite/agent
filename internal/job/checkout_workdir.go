package job

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/buildkite/agent/v4/internal/osutil"
	"github.com/buildkite/agent/v4/internal/shell"
	"github.com/buildkite/agent/v4/tracetools"
)

// prepareCheckoutWorkdir reconciles an existing checkout or clones a fresh one.
// It intentionally owns the complete existing-vs-fresh decision so checkout.go
// remains orchestration rather than accumulating another cross-cutting branch.
func (e *Executor) prepareCheckoutWorkdir(
	ctx context.Context,
	attempt *remoteMirrorAttempt,
	sparse sparseCheckout,
	mirrorDir string,
	gitCloneFlags []string,
	userSuppliedCloneFilter bool,
) (retErr error) {
	// On mirrors and dissociation:
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
	// --dissociate is safer, so it's what we want, but it can be disabled. It
	// is important even when CleanCheckout is enabled, because auto-maintenance
	// can happen on the mirror at any time!

	existingGitDir := filepath.Join(e.shell.Getwd(), ".git")
	if osutil.FileExists(existingGitDir) {
		if _, err := e.updateRemoteURL(ctx, "", e.Repository); err != nil {
			return fmt.Errorf("setting origin: %w", err)
		}

		if mirrorDir == "" && e.GitMirrorsPath != "" {
			// A bypassed mirror must not remain reachable through a stale alternate.
			if err := e.traceOp(ctx, "git.dissociate", func(ctx context.Context) error {
				return e.dissociateIfNeeded(ctx, existingGitDir)
			}); err != nil {
				return fmt.Errorf("dissociating bypassed mirror: %w", err)
			}
		} else if mirrorDir != "" {
			switch e.GitMirrorCheckoutMode {
			case "dissociate":
				if err := e.traceOp(ctx, "git.dissociate", func(ctx context.Context) error {
					return e.dissociateIfNeeded(ctx, existingGitDir)
				}); err != nil {
					return fmt.Errorf("dissociating existing reference clone: %w", err)
				}
			case "reference":
				if err := e.reassociateIfNeeded(ctx, existingGitDir, mirrorDir); err != nil {
					return fmt.Errorf("reassociating existing clone: %w", err)
				}
			}
		}
		return nil
	}

	if mirrorDir != "" {
		gitCloneFlags = append(gitCloneFlags, "--reference", mirrorDir)
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
	if mirrorDir != "" {
		mirrorMode = e.GitMirrorCheckoutMode
	}
	tracetools.AddAttributes(cloneSpan, map[string]string{
		"git.mirror_mode":     mirrorMode,
		"git.sparse":          strconv.FormatBool(sparse.active()),
		"git.blobless_filter": strconv.FormatBool(hasPartialFilterFlags(gitCloneFlags)),
	})
	defer func() { tracetools.FinishWithError(cloneSpan, retErr) }()

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
				return errors.Join(ctxErr, cleanupErr)
			}
			return ctxErr
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
				return nil
			}
			attempt.outcome = remoteMirrorOutcomeMiss
			if !shallowClone {
				// Keep the useful mirror clone. fetchSource will fetch only
				// the missing immutable commit from canonical.
				return nil
			}

			// A canonical fetch into a lagging shallow clone keeps the
			// mirror's old shallow boundary and can expose extra history.
			// Alternates can also make a missing mirror commit look present.
			// Re-clone so canonical owns the shallow boundary in both cases.
			e.shell.Commentf("Remote Git mirror is lagging for a shallow clone; cloning from canonical repository")
			if cleanupErr := e.removeCheckoutDir(); cleanupErr != nil {
				return cleanupErr
			}
			if err := e.createCheckoutDir(); err != nil {
				return err
			}
		} else {
			var gitErr *gitError
			if errors.As(cloneErr, &gitErr) && gitErr.Type == gitErrorCloneTimeout {
				attempt.outcome = remoteMirrorOutcomeTimeout
			} else {
				attempt.outcome = remoteMirrorOutcomeError
			}
			if cleanupErr := e.removeCheckoutDir(); cleanupErr != nil {
				return errors.Join(cloneErr, cleanupErr)
			}
			if err := e.createCheckoutDir(); err != nil {
				return errors.Join(cloneErr, err)
			}
			e.shell.Commentf("Remote Git mirror clone failed; falling back to canonical repository")
		}
	}

	if err := cloneCheckout(nil, e.Repository); err != nil {
		return fmt.Errorf("cloning git repository: %w", err)
	}
	return nil
}

func isFreshCloneRemoteMirrorAttempt(attempt *remoteMirrorAttempt) bool {
	return attempt != nil &&
		attempt.site == remoteMirrorSiteFreshClone &&
		attempt.outcome == remoteMirrorOutcomeNotReached
}
