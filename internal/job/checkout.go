package job

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"time"

	"github.com/buildkite/agent/v4/internal/experiments"
	"github.com/buildkite/agent/v4/internal/osutil"
	"github.com/buildkite/agent/v4/internal/redact"
	"github.com/buildkite/agent/v4/internal/shell"
	"github.com/buildkite/agent/v4/tracetools"
	"github.com/buildkite/roko"
	"github.com/buildkite/shellwords"
)

// CheckoutPhase creates the build directory and makes sure we're running the
// build at the right commit.
func (e *Executor) CheckoutPhase(ctx context.Context) (retErr error) {
	span, ctx := tracetools.StartSpanFromContext(ctx, "checkout", e.TracingBackend)
	defer func() { span.FinishWithError(retErr) }()

	if err := e.executeGlobalHook(ctx, "pre-checkout"); err != nil {
		return err
	}

	if err := e.executePluginHook(ctx, "pre-checkout", e.pluginCheckouts); err != nil {
		return err
	}

	// Remove the checkout directory if BUILDKITE_CLEAN_CHECKOUT is present
	if e.CleanCheckout {
		e.shell.Headerf("Cleaning pipeline checkout")
		if err := e.removeCheckoutDir(); err != nil {
			return err
		}
	}

	e.shell.Headerf("Preparing working directory")

	// If we have a blank repository then use a temp dir for builds
	if e.Repository == "" {
		buildDir, err := os.MkdirTemp("", "buildkite-job-"+e.JobID)
		if err != nil {
			return err
		}
		e.shell.Env.Set("BUILDKITE_BUILD_CHECKOUT_PATH", buildDir)

		// Track the directory so we can remove it at the end of the job
		e.cleanupDirs = append(e.cleanupDirs, buildDir)
	}

	// Make sure the build directory exists
	if err := e.createCheckoutDir(); err != nil {
		return err
	}

	if err := e.checkout(ctx); err != nil {
		return err
	}

	if err := e.sendCommitToBuildkite(ctx); err != nil {
		e.shell.OptionalWarningf("git-commit-resolution-failed", "Couldn't send commit information to Buildkite: %v", err)
	}

	// Store the current value of BUILDKITE_BUILD_CHECKOUT_PATH, so we can detect if
	// one of the post-checkout hooks changed it.
	previousCheckoutPath, exists := e.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH")
	if !exists {
		e.shell.Printf("Could not determine previous checkout path from BUILDKITE_BUILD_CHECKOUT_PATH")
	}

	// Run post-checkout hooks
	if err := e.executeGlobalHook(ctx, "post-checkout"); err != nil {
		return err
	}

	if err := e.executeLocalHook(ctx, "post-checkout"); err != nil {
		return err
	}

	if err := e.executePluginHook(ctx, "post-checkout", e.pluginCheckouts); err != nil {
		return err
	}

	// Capture the new checkout path so we can see if it's changed.
	newCheckoutPath, _ := e.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH")

	// If the working directory has been changed by a hook, log and switch to it
	if previousCheckoutPath != "" && previousCheckoutPath != newCheckoutPath {
		e.shell.Headerf("A post-checkout hook has changed the working directory to \"%s\"", newCheckoutPath)

		if err := e.shell.Chdir(newCheckoutPath); err != nil {
			return err
		}
	}

	return nil
}

// checkout runs checkout hook or default checkout logic
func (e *Executor) checkout(ctx context.Context) error {
	if e.SkipCheckout {
		e.shell.Commentf("Skipping checkout, BUILDKITE_SKIP_CHECKOUT is set")
		return nil
	}

	// There can only be one checkout hook, either plugin or global, in that order
	switch {
	case e.hasPluginHook("checkout"):
		if err := e.executePluginHook(ctx, "checkout", e.pluginCheckouts); err != nil {
			return err
		}
	case e.hasGlobalHook("checkout"):
		if err := e.executeGlobalHook(ctx, "checkout"); err != nil {
			return err
		}
	default:
		if e.Repository == "" {
			e.shell.Commentf("Skipping checkout, BUILDKITE_REPO is empty")
			break
		}

		// Fail fast before any git work if git-lfs is required but missing.
		// This operation only handles default checkout behavior, so it's possible for a custom checkout hook to require git-lfs but not have this check. That's a bit unfortunate, but we can add it to custom hooks later if needed.
		//
		// We probe via `git lfs version` rather than looking up `git-lfs` on
		// PATH directly: git resolves subcommands via GIT_EXEC_PATH before
		// falling back to PATH, so on platforms where git-lfs is bundled
		// alongside git (notably Git for Windows) the binary is reachable to
		// `git lfs ...` even when a PATH lookup would miss it. This matches
		// the resolution path used by the actual LFS commands later.
		if e.GitLFSEnabled {
			// Leave stderr visible: when this probe fails it is almost always
			// a misconfigured agent environment, and git's specific message
			// (e.g. "'lfs' is not a git command") is the fastest diagnostic.
			if _, err := e.shell.Command("git", "lfs", "version").RunAndCaptureStdout(ctx, shell.ShowStderr(true)); err != nil {
				return fmt.Errorf("BUILDKITE_GIT_LFS_ENABLED=true but `git lfs version` failed; git-lfs may not be installed or not resolvable by git: %w", err)
			}
		}

		maxAttempts := e.CheckoutAttempts
		if maxAttempts <= 0 {
			maxAttempts = 6
		}

		if err := roko.NewRetrier(
			roko.WithMaxAttempts(maxAttempts),
			roko.WithStrategy(roko.Exponential(2*time.Second, 0)),
			roko.WithJitter(),
		).DoWithContext(ctx, func(r *roko.Retrier) error {
			err := e.runDefaultCheckoutAttempt(ctx)
			if err == nil {
				return nil
			}

			var errLockTimeout ErrTimedOutAcquiringLock
			var errGit *gitError

			switch {
			case errors.Is(err, errCheckoutAttemptTimedOut):
				// The per-attempt timeout fired and git was signal-killed.
				// Treat this like a generic transient failure: warn, clean
				// the checkout dir, and let the retrier try again.
				e.shell.Warningf("Checkout failed! %s (%s)", err, r)

				if err := e.removeCheckoutDir(); err != nil {
					e.shell.Warningf("Failed to remove checkout dir while cleaning up after a checkout error: %v", err)
				}

				if err := e.createCheckoutDir(); err != nil {
					return err
				}

			case shell.IsExitError(err) && shell.ExitCode(err) == -1:
				e.shell.Warningf("Checkout was interrupted by a signal")
				r.Break()

			case errors.As(err, &errLockTimeout):
				e.shell.Warningf("Checkout could not acquire the %s lock before timing out", errLockTimeout.Name)
				r.Break()
				// 94 chosen by fair die roll
				return &shell.ExitError{Code: 94, Err: err}

			case errors.Is(err, context.Canceled):
				e.shell.Warningf("Checkout was cancelled")
				r.Break()

			case errors.Is(ctx.Err(), context.Canceled):
				e.shell.Warningf("Checkout was cancelled due to context cancellation")
				r.Break()

			case errors.As(err, &errGit):
				if errGit.WasRetried {
					// This error has already been retried, so don't retry it again
					// Also don't print the retrier information, as it will be confusing -- it'll say "Attempt 1/3" but
					// we won't actually be retrying it
					e.shell.Warningf("Checkout failed! %s", err)
					r.Break()
				} else {
					e.shell.Warningf("Checkout failed! %s (%s)", err, r)
				}

				switch errGit.Type {
				case gitErrorClean, gitErrorCleanSubmodules, gitErrorClone,
					gitErrorCheckoutRetryClean, gitErrorFetchRetryClean,
					gitErrorFetchBadObject:
					// Checkout can fail because of corrupted files in the checkout which can leave the agent in a state where it
					// keeps failing. This removes the checkout dir, which means the next checkout will be a lot slower (clone vs
					// fetch), but hopefully will allow the agent to self-heal
					if err := e.removeCheckoutDir(); err != nil {
						e.shell.Warningf("Failed to remove checkout dir while cleaning up after a checkout error: %v", err)
					}

					// Now make sure the build directory exists again before we try to checkout again, or proceed and run hooks
					// which presume the checkout dir exists
					if err := e.createCheckoutDir(); err != nil {
						return err
					}

				default:
					// Otherwise, don't clean the checkout dir
					return err
				}

			default:
				e.shell.Warningf("Checkout failed! %s (%s)", err, r)

				// If it's some kind of error that we don't know about, clean the checkout dir just to be safe
				if err := e.removeCheckoutDir(); err != nil {
					e.shell.Warningf("Failed to remove checkout dir while cleaning up after a checkout error: %v", err)
				}

				// Now make sure the build directory exists again before we try to checkout again, or proceed and run hooks
				// which presume the checkout dir exists
				if err := e.createCheckoutDir(); err != nil {
					return err
				}
			}

			return err
		}); err != nil {
			return err
		}
	}

	// After everything, we need to refresh checkout root.
	// This is because checkout hook might re-create the checkout root folder entirely, deprecating e.checkoutRoot.
	if err := e.refreshCheckoutRoot(); err != nil {
		return err
	}

	return nil
}

// errCheckoutAttemptTimedOut is a sentinel marker, joined into the error
// returned by runDefaultCheckoutAttempt when the per-attempt timeout fires.
// The retry loop matches it explicitly so a timeout-killed git process is
// retried instead of being treated like a user signal.
var errCheckoutAttemptTimedOut = errors.New("checkout attempt timed out")

// runDefaultCheckoutAttempt runs defaultCheckoutPhase, applying a per-attempt
// timeout if BUILDKITE_GIT_CHECKOUT_TIMEOUT is set. On timeout the returned
// error is joined with errCheckoutAttemptTimedOut so the retry loop can
// distinguish a timeout-kill from other signal-terminated processes.
func (e *Executor) runDefaultCheckoutAttempt(ctx context.Context) error {
	if e.GitCheckoutTimeout <= 0 {
		return e.defaultCheckoutPhase(ctx)
	}

	timeout := time.Duration(e.GitCheckoutTimeout) * time.Second
	attemptCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	err := e.defaultCheckoutPhase(attemptCtx)
	if err != nil && attemptCtx.Err() == context.DeadlineExceeded && ctx.Err() == nil {
		return fmt.Errorf("%w after %s: %w", errCheckoutAttemptTimedOut, timeout, err)
	}
	return err
}

// defaultCheckoutPhase is called by the CheckoutPhase if no global or plugin checkout
// hook exists. It performs the default checkout on the Repository provided in the config
func (e *Executor) defaultCheckoutPhase(ctx context.Context) (retErr error) {
	span, _ := tracetools.StartSpanFromContext(ctx, "repo-checkout", e.TracingBackend)
	span.AddAttributes(map[string]string{
		"checkout.repo_name": redact.URLCredentials(e.Repository),
		"checkout.refspec":   e.RefSpec,
		"checkout.commit":    e.Commit,
	})
	defer func() { span.FinishWithError(retErr) }()

	if e.SSHKeyscan {
		addRepositoryHostToSSHKnownHosts(ctx, e.shell, e.Repository)
	}

	sshKeyPath, cleanupSSHKey, err := e.prepareGitSSHKey()
	if err != nil {
		return fmt.Errorf("preparing git ssh key: %w", err)
	}
	if cleanupSSHKey != nil {
		defer func() {
			if cleanupErr := cleanupSSHKey(); cleanupErr != nil {
				cleanupErr = fmt.Errorf("cleaning up git ssh key %q: %w", sshKeyPath, cleanupErr)
				retErr = errors.Join(retErr, cleanupErr)
			}
		}()
	}

	var mirrorDir string

	// If we can, get a mirror of the git repository to use for reference later
	if e.GitMirrorsPath != "" && e.Repository != "" {
		span.AddAttributes(map[string]string{"checkout.is_using_git_mirrors": "true"})
		var err error
		mirrorDir, err = e.getOrUpdateMirrorDir(ctx, e.Repository)
		if err != nil {
			return fmt.Errorf("getting/updating git mirror: %w", err)
		}

		e.shell.Env.Set("BUILDKITE_REPO_MIRROR", mirrorDir)
	}

	// Make sure the build directory exists and that we change directory into it
	if err := e.createCheckoutDir(); err != nil {
		return fmt.Errorf("creating checkout dir: %w", err)
	}

	// Resolve the cone paths to check out (nil means a full checkout).
	sparsePaths := e.resolveSparseCheckout(ctx)

	// Split the git clone flags into an array of strings, so we can append
	// additional flags if needed (e.g., --reference, --dissociate, --sparse, --filter=blob:none).
	gitCloneFlags, err := shellwords.Split(e.GitCloneFlags)
	if err != nil {
		return fmt.Errorf("splitting --git-clone-flags %q: %w", e.GitCloneFlags, err)
	}

	// Snapshot whether the user supplied their own --filter before we
	// potentially append one — the fetch-side decision depends on the
	// original user-supplied state, not on flags we auto-add here.
	userSuppliedCloneFilter := hasPartialFilterFlags(gitCloneFlags)

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

	// Does the git directory exist?
	existingGitDir := filepath.Join(e.shell.Getwd(), ".git")
	if osutil.FileExists(existingGitDir) {
		// Ensure the origin matches the configured repo, so we can
		// gracefully handle repository renames.
		if _, err := e.updateRemoteURL(ctx, "", e.Repository); err != nil {
			return fmt.Errorf("setting origin: %w", err)
		}

		if mirrorDir != "" {
			switch e.GitMirrorCheckoutMode {
			case "dissociate":
				// If the existing repo is still relying on the reference, then
				// "dissociate" it (git repack, and delete the alternates file).
				if err := e.dissociateIfNeeded(ctx, existingGitDir); err != nil {
					return fmt.Errorf("dissociating existing reference clone: %w", err)
				}
			case "reference":
				// If the existing repo does not have a reference to the mirror,
				// create one. Existing objects don't need cleaning up.
				if err := e.reassociateIfNeeded(ctx, existingGitDir, mirrorDir); err != nil {
					return fmt.Errorf("reassociating existing clone: %w", err)
				}
			}
		}

	} else { // the .git directory does not already exist

		// Compute the clone flags. For mirrors we need --reference, and usually
		// --dissociate.
		if mirrorDir != "" {
			gitCloneFlags = append(gitCloneFlags, "--reference", mirrorDir)
			if e.GitMirrorCheckoutMode == "dissociate" {
				gitCloneFlags = append(gitCloneFlags, "--dissociate")
			}
		}

		// When sparse checkout applies, add two clone flags:
		//   --sparse             clone in sparse mode
		//   --filter=blob:none   make it a partial clone, so blobs outside the
		//                        sparse set aren't downloaded up front
		// Each flag is added only if the user hasn't already supplied their own.
		if len(sparsePaths) > 0 {
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

		// Do the clone.
		if err := gitClone(ctx, e.shell, gitCloneFlags, e.Repository, "."); err != nil {
			return fmt.Errorf("cloning git repository: %w", err)
		}
	}

	// Git clean prior to checkout, we do this even if submodules have been
	// disabled to ensure previous submodules are cleaned up
	if hasGitSubmodules(e.shell) {
		if err := gitCleanSubmodules(ctx, e.shell, e.GitCleanFlags); err != nil {
			return fmt.Errorf("cleaning git submodules: %w", err)
		}
	}

	if err := gitClean(ctx, e.shell, e.GitCleanFlags); err != nil {
		return fmt.Errorf("cleaning git repository: %w", err)
	}

	// Install LFS filter before fetch so the filter is registered before any
	// network operation, following the conventional git-lfs setup order.
	if e.GitLFSEnabled {
		e.shell.Commentf("Installing Git LFS filter")
		if err := e.shell.Command("git", "lfs", "install", "--local").Run(ctx); err != nil {
			return fmt.Errorf("installing git lfs filter: %w", err)
		}
	}

	// Parse the fetch flags into tokens so we can check for a user-supplied --filter flag.
	gitFetchFlags, err := shellwords.Split(e.GitFetchFlags)
	if err != nil {
		return fmt.Errorf("splitting --git-fetch-flags %q: %w", e.GitFetchFlags, err)
	}

	addBloblessFilter := len(sparsePaths) > 0 &&
		!userSuppliedCloneFilter &&
		!hasPartialFilterFlags(gitFetchFlags)
	if err := e.fetchSource(ctx, addBloblessFilter); err != nil {
		return err
	}

	if err := e.verifyCommit(ctx); err != nil {
		return err
	}

	sparseCheckoutActive, err := e.setupSparseCheckout(ctx, sparsePaths)
	if err != nil {
		return err
	}

	gitCheckoutFlags := e.GitCheckoutFlags

	if e.Commit == "HEAD" {
		if err := gitCheckout(ctx, e.shell, gitCheckoutFlags, "FETCH_HEAD"); err != nil {
			return fmt.Errorf("checking out FETCH_HEAD: %w", err)
		}
	} else {
		if err := gitCheckout(ctx, e.shell, gitCheckoutFlags, e.Commit); err != nil {
			return fmt.Errorf("checking out commit %q: %w", e.Commit, err)
		}
	}

	gitSubmodules := false
	if hasGitSubmodules(e.shell) {
		switch {
		case sparseCheckoutActive:
			e.shell.Commentf("Submodule initialization skipped during sparse checkout")
		case e.GitSubmodules:
			e.shell.Commentf("Git submodules detected")
			gitSubmodules = true
		default:
			e.shell.OptionalWarningf("submodules-disabled", "This repository has submodules, but submodules are disabled")
		}
	}

	if gitSubmodules {
		// `submodule sync` will ensure the .git/config
		// matches the .gitmodules file.  The command
		// is only available in git version 1.8.1, so
		// if the call fails, continue the job
		// script, and show an informative error.
		if err := e.shell.Command("git", "submodule", "sync", "--recursive").Run(ctx); err != nil {
			gitVersionOutput, _ := e.shell.Command("git", "--version").RunAndCaptureStdout(ctx)
			e.shell.Warningf("Failed to recursively sync git submodules. This is most likely because you have an older version of git installed (" + gitVersionOutput + ") and you need version 1.8.1 and above. If you're using submodules, it's highly recommended you upgrade if you can.")
		}

		args := []string{}
		for _, config := range e.GitSubmoduleCloneConfig {
			// -c foo=bar is valid, -c foo= is valid, -c foo is valid, but...
			// -c (nothing) is invalid.
			// This could happen because the env var was set to an empty value.
			if config == "" {
				continue
			}
			args = append(args, "-c", config)
		}

		// Checking for submodule repositories
		submoduleRepos, err := gitEnumerateSubmoduleURLs(ctx, e.shell)
		if err != nil {
			e.shell.Warningf("Failed to enumerate git submodules: %v", err)
		} else {
			mirrorSubmodules := e.GitMirrorsPath != ""
			for _, repository := range submoduleRepos {
				// submodules might need their fingerprints verified too
				if e.SSHKeyscan {
					addRepositoryHostToSSHKnownHosts(ctx, e.shell, repository)
				}

				if !mirrorSubmodules {
					continue
				}
				// It's all mirrored submodules for the rest of the loop.

				mirrorDir, err := e.getOrUpdateMirrorDir(ctx, repository)
				if err != nil {
					return fmt.Errorf("getting/updating mirror dir for submodules: %w", err)
				}

				// Switch back to the checkout dir, doing other operations from GitMirrorsPath will fail.
				if err := e.createCheckoutDir(); err != nil {
					return fmt.Errorf("creating checkout dir: %w", err)
				}

				submoduleArgs := slices.Clone(args)
				if mirrorDir != "" {
					submoduleArgs = append(submoduleArgs, "submodule", "update", "--init", "--recursive", "--force", "--reference", mirrorDir)
					if e.GitMirrorCheckoutMode == "dissociate" {
						submoduleArgs = append(submoduleArgs, "--dissociate")
					}
				} else {
					// Fall back to a clean update, rather than failing the checkout and therefore the build
					submoduleArgs = append(submoduleArgs, "submodule", "update", "--init", "--recursive", "--force")
				}

				if err := e.shell.Command("git", submoduleArgs...).Run(ctx); err != nil {
					return fmt.Errorf("updating submodules: %w", err)
				}
			}

			if !mirrorSubmodules {
				args = append(args, "submodule", "update", "--init", "--recursive", "--force")
				if err := e.shell.Command("git", args...).Run(ctx); err != nil {
					return fmt.Errorf("updating submodules: %w", err)
				}
			}

			cmd := e.shell.Command("git", "submodule", "foreach", "--recursive", "git reset --hard")
			if err := cmd.Run(ctx); err != nil {
				return fmt.Errorf("resetting submodules: %w", err)
			}
		}
	}

	// When sparse-checkout is active, scope LFS to the same paths so we don't
	// pull objects outside the sparse set (SUP-6529). If sparse fell back to a
	// full checkout (e.g. git < 2.27), fetch unscoped so files outside the
	// requested paths still get their LFS content.
	if e.GitLFSEnabled {
		lfsArgs := gitLFSFetchCheckoutArgs{
			Shell: e.shell,
			Retry: true,
		}
		if sparseCheckoutActive {
			lfsArgs.Include = cleanGitSparseCheckoutPaths(e.GitSparseCheckoutPaths)
		}
		if err := gitLFSFetchCheckout(ctx, lfsArgs); err != nil {
			return err
		}
	}

	// Git clean after checkout. We need to do this because submodules could have
	// changed in between the last checkout and this one. A double clean is the only
	// good solution to this problem that we've found
	e.shell.Commentf("Cleaning again to catch any post-checkout changes")

	if err := gitClean(ctx, e.shell, e.GitCleanFlags); err != nil {
		return fmt.Errorf("cleaning repository post-checkout: %w", err)
	}

	if gitSubmodules {
		if err := gitCleanSubmodules(ctx, e.shell, e.GitCleanFlags); err != nil {
			return fmt.Errorf("cleaning submodules post-checkout: %w", err)
		}
	}

	if _, hasToken := e.shell.Env.Get("BUILDKITE_AGENT_ACCESS_TOKEN"); !hasToken {
		e.shell.Warningf("Skipping sending Git information to Buildkite as $BUILDKITE_AGENT_ACCESS_TOKEN is missing")
		return nil
	}

	// resolve BUILDKITE_COMMIT based on the local git repo
	if experiments.IsEnabled(ctx, experiments.ResolveCommitAfterCheckout) {
		e.shell.Commentf("Using resolve-commit-after-checkout experiment 🧪")
		e.resolveCommit(ctx)
	}

	return nil
}

// createCheckoutDir checks for the existence of a directory at
// $BUILDKITE_BUILD_CHECKOUT_PATH, and creates it if it does not exist.
// It opens the checkout directory as an [os.Root], saved to e.checkoutRoot.
// It then changes e.shell's working directory to the checkout directory.
func (e *Executor) createCheckoutDir() error {
	checkoutPath, _ := e.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH")

	if !osutil.FileExists(checkoutPath) {
		e.shell.Commentf("Creating %q", checkoutPath)
		// Actual file permissions will be reduced by umask, and won't be 0o777 unless the user has manually changed the umask to 000
		if err := os.MkdirAll(checkoutPath, 0o777); err != nil {
			return err
		}
	}

	if err := e.refreshCheckoutRoot(); err != nil {
		return err
	}

	if e.shell.Getwd() != checkoutPath {
		if err := e.shell.Chdir(checkoutPath); err != nil {
			return err
		}
	}

	return nil
}

// refreshCheckoutRoot refreshes e.checkoutRoot
func (e *Executor) refreshCheckoutRoot() error {
	checkoutPath, _ := e.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH")
	if e.checkoutRoot != nil {
		if err := e.checkoutRoot.Close(); err != nil {
			// While it's unlikely, it's not a blocking error
			e.shell.Warningf("unable to close existing checkoutRoot during refreshCheckoutRoot: %w", err)
		}
	}
	root, err := os.OpenRoot(checkoutPath)
	if err != nil {
		return fmt.Errorf("opening checkout path as root: %w", err)
	}
	// This cleanup is largely ornamental, since the executor pointer only
	// becomes unreachable when the bootstrap exits.
	runtime.AddCleanup(e, func(r *os.Root) { _ = r.Close() }, root)
	e.checkoutRoot = root
	return nil
}

func (e *Executor) removeCheckoutDir() error {
	checkoutPath, _ := e.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH")

	if e.checkoutRoot != nil {
		_ = e.checkoutRoot.Close()
		e.checkoutRoot = nil
	}

	// on windows, sometimes removing large dirs can fail for various reasons
	// for instance having files open
	// see https://github.com/golang/go/issues/20841
	for range 10 {
		e.shell.Commentf("Removing %s", checkoutPath)
		if err := os.RemoveAll(checkoutPath); err != nil {
			e.shell.Errorf("Failed to remove \"%s\" (%s)", checkoutPath, err)
		} else {
			if _, err := os.Stat(checkoutPath); os.IsNotExist(err) {
				return nil
			} else {
				e.shell.Errorf("Failed to remove %s", checkoutPath)
			}
		}
		e.shell.Commentf("Waiting 10 seconds")
		<-time.After(time.Second * 10)
	}

	return fmt.Errorf("failed to remove %s", checkoutPath)
}
