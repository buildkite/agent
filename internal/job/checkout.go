package job

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/buildkite/agent/v3/internal/experiments"
	"github.com/buildkite/agent/v3/internal/osutil"
	"github.com/buildkite/agent/v3/internal/self"
	"github.com/buildkite/agent/v3/internal/shell"
	"github.com/buildkite/agent/v3/tracetools"
	"github.com/buildkite/roko"
)

// configureGitCredentialHelper sets up the agent to use a git credential helper that calls the Buildkite Agent API
// asking for a Github App token to use when cloning. This feature is turned on serverside
func (e *Executor) configureGitCredentialHelper(ctx context.Context) error {
	// credential.useHttpPath is a git config setting that tells git to tell the credential helper the full URL of the repo
	// this means that we can pass the repo being cloned up to the BK API, which can then choose (or not, if it's not permitted)
	// to return a token for that repo.
	//
	// This is important for the case where a user clones multiple repos in a step - ie, if we always crammed
	// os.Getenv("BUILDKITE_REPO") into credential helper, we'd only ever get a token for the repo that the step is running
	// in, and not for any other repos that the step might clone.
	err := e.shell.Command("git", "config", "--global", "credential.useHttpPath", "true").Run(ctx, shell.ShowPrompt(false))
	if err != nil {
		return fmt.Errorf("enabling git credential.useHttpPath: %w", err)
	}

	helper := fmt.Sprintf(`%s git-credentials-helper`, self.Path(ctx))
	err = e.shell.Command("git", "config", "--global", "credential.helper", helper).Run(ctx, shell.ShowPrompt(false))
	if err != nil {
		return fmt.Errorf("configuring git credential.helper: %w", err)
	}

	return nil
}

// Disables SSH keyscan and configures git to use HTTPS instead of SSH for github.
// We may later expand this for other SCMs.
func (e *Executor) configureHTTPSInsteadOfSSH(ctx context.Context) error {
	return e.shell.Command(
		"git", "config", "--global", "url.https://github.com/.insteadOf", "git@github.com:",
	).Run(ctx, shell.ShowPrompt(false))
}

func (e *Executor) removeCheckoutDir() error {
	checkoutPath, _ := e.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH")

	if e.checkoutRoot != nil {
		e.checkoutRoot.Close()
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
	runtime.AddCleanup(e, func(r *os.Root) { r.Close() }, root)
	e.checkoutRoot = root
	return nil
}

// CheckoutPhase creates the build directory and makes sure we're running the
// build at the right commit.
func (e *Executor) CheckoutPhase(ctx context.Context) error {
	span, ctx := tracetools.StartSpanFromContext(ctx, "checkout", e.TracingBackend)
	var err error
	defer func() { span.FinishWithError(err) }()

	if err = e.executeGlobalHook(ctx, "pre-checkout"); err != nil {
		return err
	}

	if err = e.executePluginHook(ctx, "pre-checkout", e.pluginCheckouts); err != nil {
		return err
	}

	// Remove the checkout directory if BUILDKITE_CLEAN_CHECKOUT is present
	if e.CleanCheckout {
		e.shell.Headerf("Cleaning pipeline checkout")
		if err = e.removeCheckoutDir(); err != nil {
			return err
		}
	}

	e.shell.Headerf("Preparing working directory")

	// If we have a blank repository then use a temp dir for builds
	if e.Repository == "" {
		var buildDir string
		buildDir, err = os.MkdirTemp("", "buildkite-job-"+e.JobID)
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

	err = e.sendCommitToBuildkite(ctx)
	if err != nil {
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

		if err := roko.NewRetrier(
			roko.WithMaxAttempts(3),
			roko.WithStrategy(roko.Constant(2*time.Second)),
		).DoWithContext(ctx, func(r *roko.Retrier) error {
			err := e.defaultCheckoutPhase(ctx)
			if err == nil {
				return nil
			}

			var errLockTimeout ErrTimedOutAcquiringLock
			var errGit *gitError

			switch {
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

func hasGitSubmodules(sh *shell.Shell) bool {
	return osutil.FileExists(filepath.Join(sh.Getwd(), ".gitmodules"))
}

func hasGitCommit(ctx context.Context, sh *shell.Shell, gitDir, commit string) bool {
	// Resolve commit to an actual commit object
	output, err := sh.Command("git", "--git-dir", gitDir, "rev-parse", commit+"^{commit}").RunAndCaptureStdout(ctx, shell.ShowStderr(false))
	if err != nil {
		return false
	}

	// Filter out commitish things like HEAD et al
	if strings.TrimSpace(output) != commit {
		return false
	}

	// Otherwise it's a commit in the repo
	return true
}

func (e *Executor) updateGitMirror(ctx context.Context, repository string) (string, error) {
	// Create a unique directory for the repository mirror
	mirrorDir := filepath.Join(e.GitMirrorsPath, dirForRepository(repository))
	isMainRepository := repository == e.Repository

	// Create the mirrors path if it doesn't exist
	if baseDir := filepath.Dir(mirrorDir); !osutil.FileExists(baseDir) {
		e.shell.Commentf("Creating \"%s\"", baseDir)
		// Actual file permissions will be reduced by umask, and won't be 0o777 unless the user has manually changed the umask to 000
		if err := os.MkdirAll(baseDir, 0o777); err != nil {
			return "", err
		}
	}

	if err := e.shell.Chdir(e.GitMirrorsPath); err != nil {
		return "", fmt.Errorf("failed to change directory to %q: %w", e.GitMirrorsPath, err)
	}

	lockTimeout := time.Second * time.Duration(e.GitMirrorsLockTimeout)

	if e.Debug {
		e.shell.Commentf("Acquiring mirror repository clone lock")
	}

	// Lock the mirror dir to prevent concurrent clones
	cloneCtx, canc := context.WithTimeout(ctx, lockTimeout)
	defer canc()
	mirrorCloneLock, err := e.shell.LockFile(cloneCtx, mirrorDir+".clonelock")
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return "", ErrTimedOutAcquiringLock{Name: "clone", Err: err}
		}
		return "", fmt.Errorf("unable to acquire clone lock: %w", err)
	}
	defer mirrorCloneLock.Unlock() //nolint:errcheck // Best-effort cleanup - primary unlock checked below.

	// If we don't have a mirror, we need to clone it
	if !osutil.FileExists(mirrorDir) {
		e.shell.Commentf("Cloning a mirror of the repository to %q", mirrorDir)
		flags := "--mirror " + e.GitCloneMirrorFlags
		if err := gitClone(ctx, e.shell, flags, repository, mirrorDir); err != nil {
			e.shell.Commentf("Removing mirror dir %q due to failed clone", mirrorDir)
			if err := os.RemoveAll(mirrorDir); err != nil {
				e.shell.Errorf("Failed to remove \"%s\" (%s)", mirrorDir, err)
			}
			return "", err
		}

		return mirrorDir, nil
	}

	// If it exists, immediately release the clone lock.
	if err := mirrorCloneLock.Unlock(); err != nil {
		return "", fmt.Errorf("unable to release clone lock: %w", err)
	}

	// Check if the mirror has a commit, this is atomic so should be safe to do
	if isMainRepository {
		if hasGitCommit(ctx, e.shell, mirrorDir, e.Commit) {
			e.shell.Commentf("Commit %q exists in mirror", e.Commit)
			return mirrorDir, nil
		}
	}

	if e.Debug {
		e.shell.Commentf("Acquiring mirror repository update lock")
	}

	// Lock the mirror dir to prevent concurrent updates
	updateCtx, canc := context.WithTimeout(ctx, lockTimeout)
	defer canc()
	mirrorUpdateLock, err := e.shell.LockFile(updateCtx, mirrorDir+".updatelock")
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return "", ErrTimedOutAcquiringLock{Name: "update", Err: err}
		}
		return "", fmt.Errorf("unable to acquire update lock: %w", err)
	}
	defer mirrorUpdateLock.Unlock() //nolint:errcheck // Best-effort cleanup - primary unlock checked below.

	if isMainRepository {
		// Check again after we get a lock, in case the other process has already updated
		if hasGitCommit(ctx, e.shell, mirrorDir, e.Commit) {
			e.shell.Commentf("Commit %q exists in mirror", e.Commit)
			return mirrorDir, nil
		}
	}

	e.shell.Commentf("Updating existing repository mirror to find commit %s", e.Commit)

	// Update the origin of the repository so we can gracefully handle
	// repository renames.
	urlChanged, err := e.updateRemoteURL(ctx, mirrorDir, repository)
	if err != nil {
		return "", fmt.Errorf("setting remote URL: %w", err)
	}

	if isMainRepository {
		var refspecs []string
		var retry bool

		switch {
		case e.RefSpec != "":
			// If a custom refspec is provided, use it instead of the branch
			e.shell.Commentf("Fetching and mirroring custom refspec %s", e.RefSpec)
			refspecs = []string{e.RefSpec}
		case e.PullRequest != "false" && strings.Contains(e.PipelineProvider, "github"):
			e.shell.Commentf("Fetching and mirroring pull request head from GitHub. This will be retried if it fails, as the pull request head might not be available yet — GitHub creates them asynchronously")
			var refspec string
			if e.PullRequestUsingMergeRefspec {
				refspec = fmt.Sprintf("refs/pull/%s/merge", e.PullRequest)
			} else {
				refspec = fmt.Sprintf("refs/pull/%s/head", e.PullRequest)
			}
			refspecs = []string{refspec}
			retry = true
		default:
			// Fetch the build branch from the upstream repository into the mirror.
			refspecs = []string{e.Branch}
		}

		// Fetch the refspecs from the upstream repository into the mirror.
		if err := gitFetch(ctx, gitFetchArgs{
			Shell:      e.shell,
			GitFlags:   fmt.Sprintf("--git-dir=%s", mirrorDir),
			Repository: "origin",
			RefSpecs:   refspecs,
			Retry:      retry,
		}); err != nil {
			return "", err
		}
	} else { // not the main repo.

		// This is a mirror of a submodule.
		// Update without specifying particular ref, since we don't know which
		// ref is needed for the main build.
		// (If it doesn't contain the needed ref, then the build would fail on
		// a clean host or with a clean checkout.)
		// TODO: Investigate getting the ref from the main repo and passing
		// that in here.
		cmd := e.shell.Command("git", "--git-dir", mirrorDir, "fetch", "origin")
		if err := cmd.Run(ctx); err != nil {
			return "", err
		}
	}

	if urlChanged {
		// Let's opportunistically fsck and gc.
		// 1. In case of remote URL confusion (bug introduced in #1959), and
		// 2. There's possibly some object churn when remotes are renamed.
		if err := e.shell.Command("git", "--git-dir", mirrorDir, "fsck").Run(ctx); err != nil {
			e.shell.Warningf("Couldn't run git fsck: %v", err)
		}
		if err := e.shell.Command("git", "--git-dir", mirrorDir, "gc").Run(ctx); err != nil {
			e.shell.Warningf("Couldn't run git gc: %v", err)
		}
	}

	if err := mirrorUpdateLock.Unlock(); err != nil {
		return "", fmt.Errorf("unable to release update lock: %w", err)
	}

	return mirrorDir, nil
}

type ErrTimedOutAcquiringLock struct {
	Name string
	Err  error
}

func (e ErrTimedOutAcquiringLock) Error() string {
	return fmt.Sprintf("timed out acquiring %s lock: %v", e.Name, e.Err)
}

func (e ErrTimedOutAcquiringLock) Unwrap() error { return e.Err }

// updateRemoteURL updates the URL for 'origin'. If gitDir == "", it assumes the
// local repo is in the current directory, otherwise it includes --git-dir.
// If the remote has changed, it logs some extra information. updateRemoteURL
// reports if the remote URL changed.
func (e *Executor) updateRemoteURL(ctx context.Context, gitDir, repository string) (bool, error) {
	// Update the origin of the repository so we can gracefully handle
	// repository renames.

	// First check what the existing remote is, for both logging and debugging
	// purposes.
	args := []string{"remote", "get-url", "origin"}
	if gitDir != "" {
		args = append([]string{"--git-dir", gitDir}, args...)
	}
	gotURL, err := e.shell.Command("git", args...).RunAndCaptureStdout(ctx)
	if err != nil {
		return false, err
	}

	if gotURL == repository {
		// No need to update anything
		return false, nil
	}

	gd := gitDir
	if gd == "" {
		gd = e.shell.Getwd()
	}

	e.shell.Commentf("Remote URL for git directory %s has changed (%s -> %s)!", gd, gotURL, repository)
	e.shell.Commentf("This is usually because the repository has been renamed.")
	e.shell.Commentf("If this is unexpected, you may see failures.")

	args = []string{"remote", "set-url", "origin", repository}
	if gitDir != "" {
		args = append([]string{"--git-dir", gitDir}, args...)
	}
	return true, e.shell.Command("git", args...).Run(ctx)
}

func (e *Executor) getOrUpdateMirrorDir(ctx context.Context, repository string) (string, error) {
	var mirrorDir string
	// Skip updating the Git mirror before using it?
	if e.GitMirrorsSkipUpdate {
		mirrorDir = filepath.Join(e.GitMirrorsPath, dirForRepository(repository))
		e.shell.Commentf("Skipping update and using existing mirror for repository %s at %s.", repository, mirrorDir)

		// Check if specified mirrorDir exists, otherwise the clone will fail.
		if !osutil.FileExists(mirrorDir) {
			// Fall back to a clean clone, rather than failing the clone and therefore the build
			e.shell.Commentf("No existing mirror found for repository %s at %s.", repository, mirrorDir)
			mirrorDir = ""
		}
		return mirrorDir, nil
	}

	return e.updateGitMirror(ctx, repository)
}

// defaultCheckoutPhase is called by the CheckoutPhase if no global or plugin checkout
// hook exists. It performs the default checkout on the Repository provided in the config
func (e *Executor) defaultCheckoutPhase(ctx context.Context) error {
	span, _ := tracetools.StartSpanFromContext(ctx, "repo-checkout", e.TracingBackend)
	span.AddAttributes(map[string]string{
		"checkout.repo_name": e.Repository,
		"checkout.refspec":   e.RefSpec,
		"checkout.commit":    e.Commit,
	})
	var err error
	defer func() { span.FinishWithError(err) }()

	if e.SSHKeyscan {
		addRepositoryHostToSSHKnownHosts(ctx, e.shell, e.Repository)
	}

	var mirrorDir string

	// If we can, get a mirror of the git repository to use for reference later
	if e.GitMirrorsPath != "" && e.Repository != "" {
		span.AddAttributes(map[string]string{"checkout.is_using_git_mirrors": "true"})
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

	gitCloneFlags := e.GitCloneFlags
	if mirrorDir != "" {
		gitCloneFlags += fmt.Sprintf(" --reference %q", mirrorDir)
	}

	// Does the git directory exist?
	existingGitDir := filepath.Join(e.shell.Getwd(), ".git")
	if osutil.FileExists(existingGitDir) {
		// Update the origin of the repository so we can gracefully handle
		// repository renames
		if _, err := e.updateRemoteURL(ctx, "", e.Repository); err != nil {
			return fmt.Errorf("setting origin: %w", err)
		}
	} else {
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

	gitFetchFlags := e.GitFetchFlags

	switch {
	case e.RefSpec != "":
		// If a refspec is provided then use it instead.
		// For example, `refs/not/a/head`
		e.shell.Commentf("Fetch and checkout custom refspec")
		if err := gitFetch(ctx, gitFetchArgs{
			Shell:         e.shell,
			GitFetchFlags: gitFetchFlags,
			Repository:    "origin",
			RefSpecs:      []string{e.RefSpec},
		}); err != nil {
			return fmt.Errorf("fetching refspec %q: %w", e.RefSpec, err)
		}

	case e.PullRequest != "false" && strings.Contains(e.PipelineProvider, "github"):
		var refspec string
		var retry bool

		if e.PullRequestUsingMergeRefspec {
			// Merge refspecs represents a speculative merge of the PR branch against the base branch.
			// Checking out this refspec enables testing the result of the merge before it happens.
			// If a merge conflict exists, this refspec won't be created and the fetch will fail. In this
			// case we want the job to fail earlier, rather than retrying the fetch (which adds ~2-3 mins job run time before failing)
			// Note: An outer retry loop will still retry the failed checkout 3 times before failing.
			e.shell.Commentf("Fetch and checkout pull request merge commit from GitHub")
			retry = false
			refspec = fmt.Sprintf("refs/pull/%s/merge", e.PullRequest)
		} else {
			// GitHub has a special ref which lets us fetch a pull request head, whether
			// or not it's a current head in this repository or a fork. See:
			// https://help.github.com/articles/checking-out-pull-requests-locally/#modifying-an-inactive-pull-request-locally
			e.shell.Commentf("Fetch and checkout pull request head from GitHub")
			retry = true
			refspec = fmt.Sprintf("refs/pull/%s/head", e.PullRequest)
		}
		refspecs := []string{refspec}

		if e.Commit == "HEAD" {
			// If we don't know the commit, we don't want to fetch with a fallback (otherwise FETCH_HEAD
			// will resolve during a fallback to the alphabetically earliest branch/tag - rather than the
			// correct commit for this build)
			if err := gitFetch(ctx, gitFetchArgs{
				Shell:         e.shell,
				GitFetchFlags: gitFetchFlags,
				Repository:    "origin",
				Retry:         retry,
				RefSpecs:      refspecs,
			}); err != nil {
				return fmt.Errorf("Fetching PR refspec %q: %w", refspecs, err)
			}
		} else {
			// If we know the commit, also fetch it directly. The commit might not be in the history of `refspec` if there
			// have been force pushes to the pull request, so this ensures we have it.
			// Note: this is the typical case e.Commit != HEAD.
			refspecs = append(refspecs, e.Commit)
			// We aim to eliminate network round-trip as much as possible so we use a single git fetch here.
			if err := gitFetchWithFallback(ctx, e.shell, gitFetchFlags, refspecs...); err != nil {
				return fmt.Errorf("Fetching PR refspec %q: %w", refspecs, err)
			}
		}

		gitFetchHead, _ := e.shell.Command("git", "rev-parse", "FETCH_HEAD").RunAndCaptureStdout(ctx)
		e.shell.Commentf("FETCH_HEAD is now `%s`", gitFetchHead)

	case e.Commit == "HEAD":
		// If the commit is "HEAD" then we can't do a commit-specific fetch and will
		// need to fetch the remote head and checkout the fetched head explicitly.
		e.shell.Commentf("Fetch and checkout remote branch HEAD commit")
		if err := gitFetch(ctx, gitFetchArgs{
			Shell:         e.shell,
			GitFetchFlags: gitFetchFlags,
			Repository:    "origin",
			RefSpecs:      []string{e.Branch},
		}); err != nil {
			return fmt.Errorf("fetching branch %q: %w", e.Branch, err)
		}

	default:
		// Otherwise fetch and checkout the commit directly.
		if err := gitFetchWithFallback(ctx, e.shell, gitFetchFlags, e.Commit); err != nil {
			return err
		}
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
		if e.GitSubmodules {
			e.shell.Commentf("Git submodules detected")
			gitSubmodules = true
		} else {
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
				submoduleArgs := slices.Clone(args)
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

				// Tests use a local temp path for the repository, real repositories don't. Handle both.
				var repositoryPath string
				if !osutil.FileExists(repository) {
					repositoryPath = filepath.Join(e.GitMirrorsPath, dirForRepository(repository))
				} else {
					repositoryPath = repository
				}

				if mirrorDir != "" {
					submoduleArgs = append(submoduleArgs, "submodule", "update", "--init", "--recursive", "--force", "--reference", repositoryPath)
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

// gitFetchWithFallback run git fetch for refspecs, when it fails on recoverable reason, it will retry fetching
// all heads and refs.
func gitFetchWithFallback(ctx context.Context, shell *shell.Shell, gitFetchFlags string, refspecs ...string) error {
	if len(refspecs) == 0 {
		return fmt.Errorf("no refspecs provided for git fetch")
	}

	// Try to fetch all refspecs in a single call first
	err := gitFetch(ctx, gitFetchArgs{
		Shell:         shell,
		GitFetchFlags: gitFetchFlags,
		Repository:    "origin",
		RefSpecs:      refspecs,
	})
	if err == nil {
		return nil // all refspecs worked in single fetch
	}

	if gerr := new(gitError); errors.As(err, &gerr) {
		switch gerr.Type {
		case gitErrorFetchBadReference:
			// refspecs might contains short SHA
			break
		default:
			// bail due to repository corruption or other non recoverable issue
			return fmt.Errorf("fetching refspecs %v: %w", refspecs, err)
		}
	}

	// The refspecs might be something that's not possible to fetch directly
	// (e.g. short commit hashes), so we fall back to fetching all heads and tags,
	// hoping that the refspecs are included.
	shell.Commentf("Some refspec fetches failed, trying to fetch all heads and tags")
	// By default `git fetch origin` will only fetch tags which are
	// reachable from a fetches branch. git 1.9.0+ changed `--tags` to
	// fetch all tags in addition to the default refspec, but pre 1.9.0 it
	// excludes the default refspec.
	gitFetchRefspec, err := shell.Command("git", "config", "remote.origin.fetch").RunAndCaptureStdout(ctx)
	if err != nil {
		return fmt.Errorf("getting remote.origin.fetch: %w", err)
	}

	if err := gitFetch(ctx, gitFetchArgs{
		Shell:         shell,
		GitFetchFlags: gitFetchFlags,
		Repository:    "origin",
		Retry:         true,
		RefSpecs:      []string{gitFetchRefspec, "+refs/tags/*:refs/tags/*"},
	}); err != nil {
		return fmt.Errorf("fetching refspecs %v: %w", refspecs, err)
	}

	return nil
}

const CommitMetadataKey = "buildkite:git:commit"

// sendCommitToBuildkite sends commit information (commit, author, subject, body) to Buildkite, as the BK backend doesn't
// have access to user's VCSes. To do this, we set a special meta-data key in the build, but only if it isn't already present
// Functionally, this means that the first job in a build (usually a pipeline upload or similar) will push the commit info
// to buildkite, which uses this info to display commit info in the UI eg in the title for the build
// note that we bail early if the key already exists, as we don't want to overwrite it
func (e *Executor) sendCommitToBuildkite(ctx context.Context) error {
	e.shell.Commentf("Checking to see if git commit information needs to be sent to Buildkite...")

	commitResolved, _ := e.shell.Env.Get("BUILDKITE_COMMIT_RESOLVED")
	if commitResolved == "true" {
		// we can skip the metadata shenanigans here and push straight through
		e.shell.Commentf("BUILDKITE_COMMIT is already resolved and meta-data populated, skipping")
		return nil
	}

	cmd := e.shell.Command(self.Path(ctx), "meta-data", "exists", CommitMetadataKey)
	if err := cmd.Run(ctx); err == nil {
		// Command exited 0, ie the key exists, so we don't need to send it again
		e.shell.Commentf("Git commit information has already been sent to Buildkite")
		return nil
	}

	e.shell.Commentf("Sending Git commit information back to Buildkite")
	// Format:
	//
	// commit 0123456789abcdef0123456789abcdef01234567
	// abbrev-commit 0123456789
	// Author: John Citizen <john@example.com>
	//
	//    Subject of the commit message
	//
	//    Body of the commit message, which
	//    may span multiple lines.
	gitArgs := []string{
		"--no-pager",
		"log",
		"-1",
		e.Commit,
		"-s", // --no-patch was introduced in v1.8.4 in 2013, but e.g. CentOS 7 isn't there yet
		"--no-color",
		"--format=commit %H%nabbrev-commit %h%nAuthor: %an <%ae>%n%n%w(0,4,4)%B",
	}
	out, err := e.shell.Command("git", gitArgs...).RunAndCaptureStdout(ctx)
	if err != nil {
		return fmt.Errorf("getting git commit information: %w", err)
	}

	stdin := strings.NewReader(out)
	cmd = e.shell.CloneWithStdin(stdin).Command(self.Path(ctx), "meta-data", "set", CommitMetadataKey)
	if err := cmd.Run(ctx); err != nil {
		return fmt.Errorf("sending git commit information to Buildkite: %w", err)
	}

	return nil
}

func (e *Executor) resolveCommit(ctx context.Context) {
	commitRef, _ := e.shell.Env.Get("BUILDKITE_COMMIT")
	if commitRef == "" {
		e.shell.Warningf("BUILDKITE_COMMIT was empty")
		return
	}
	cmdOut, err := e.shell.Command("git", "rev-parse", commitRef).RunAndCaptureStdout(ctx)
	if err != nil {
		e.shell.Warningf("Error running git rev-parse %q: %v", commitRef, err)
		return
	}
	trimmedCmdOut := strings.TrimSpace(string(cmdOut))
	if trimmedCmdOut != commitRef {
		e.shell.Commentf("Updating BUILDKITE_COMMIT from %q to %q", commitRef, trimmedCmdOut)
		e.shell.Env.Set("BUILDKITE_COMMIT", trimmedCmdOut)
	}
}
