package job

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/buildkite/agent/v3/internal/experiments"
	"github.com/buildkite/agent/v3/internal/job/shell"
	"github.com/buildkite/agent/v3/internal/utils"
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
	err := e.shell.RunWithoutPrompt(ctx, "git", "config", "--global", "credential.useHttpPath", "true")
	if err != nil {
		return fmt.Errorf("enabling git credential.useHttpPath: %w", err)
	}

	buildkiteAgent, err := os.Executable()
	if err != nil {
		return fmt.Errorf("getting executable path: %w", err)
	}

	helper := fmt.Sprintf(`%s git-credentials-helper`, buildkiteAgent)
	err = e.shell.RunWithoutPrompt(ctx, "git", "config", "--global", "credential.helper", helper)
	if err != nil {
		return fmt.Errorf("configuring git credential.helper: %w", err)
	}

	return nil
}

// Disables SSH keyscan and configures git to use HTTPS instead of SSH for github.
// We may later expand this for other SCMs.
func (e *Executor) configureHTTPSInsteadOfSSH(ctx context.Context) error {
	return e.shell.RunWithoutPrompt(ctx,
		"git", "config", "--global", "url.https://github.com/.insteadOf", "git@github.com:",
	)
}

func (e *Executor) removeCheckoutDir() error {
	checkoutPath, _ := e.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH")

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

	return fmt.Errorf("Failed to remove %s", checkoutPath)
}

func (e *Executor) createCheckoutDir() error {
	checkoutPath, _ := e.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH")

	if !utils.FileExists(checkoutPath) {
		e.shell.Commentf("Creating \"%s\"", checkoutPath)
		// Actual file permissions will be reduced by umask, and won't be 0777 unless the user has manually changed the umask to 000
		if err := os.MkdirAll(checkoutPath, 0777); err != nil {
			return err
		}
	}

	if e.shell.Getwd() != checkoutPath {
		if err := e.shell.Chdir(checkoutPath); err != nil {
			return err
		}
	}

	return nil
}

// CheckoutPhase creates the build directory and makes sure we're running the
// build at the right commit.
func (e *Executor) CheckoutPhase(ctx context.Context) error {
	span, ctx := tracetools.StartSpanFromContext(ctx, "checkout", e.ExecutorConfig.TracingBackend)
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
	if e.ExecutorConfig.Repository == "" {
		var buildDir string
		buildDir, err = os.MkdirTemp("", "buildkite-job-"+e.ExecutorConfig.JobID)
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
		if e.ExecutorConfig.Repository == "" {
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

			switch {
			case shell.IsExitError(err) && shell.GetExitCode(err) == -1:
				e.shell.Warningf("Checkout was interrupted by a signal")
				r.Break()

			case errors.Is(err, context.Canceled):
				e.shell.Warningf("Checkout was cancelled")
				r.Break()

			case errors.Is(ctx.Err(), context.Canceled):
				e.shell.Warningf("Checkout was cancelled due to context cancellation")
				r.Break()

			default:
				e.shell.Warningf("Checkout failed! %s (%s)", err, r)

				// Specifically handle git errors
				if ge := new(gitError); errors.As(err, &ge) {
					switch ge.Type {
					// These types can fail because of corrupted checkouts
					case gitErrorClean, gitErrorCleanSubmodules, gitErrorClone,
						gitErrorCheckoutRetryClean, gitErrorFetchRetryClean,
						gitErrorFetchBadObject:
					// Otherwise, don't clean the checkout dir
					default:
						return err
					}
				}

				// Checkout can fail because of corrupted files in the checkout
				// which can leave the agent in a state where it keeps failing
				// This removes the checkout dir, which means the next checkout
				// will be a lot slower (clone vs fetch), but hopefully will
				// allow the agent to self-heal
				if err := e.removeCheckoutDir(); err != nil {
					e.shell.Printf("Failed to remove checkout dir while cleaning up after a checkout error.")
				}

				// Now make sure the build directory exists again before we try
				// to checkout again, or proceed and run hooks which presume the
				// checkout dir exists
				if err := e.createCheckoutDir(); err != nil {
					return err
				}
			}

			return err
		}); err != nil {
			return err
		}
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

func hasGitSubmodules(sh *shell.Shell) bool {
	return utils.FileExists(filepath.Join(sh.Getwd(), ".gitmodules"))
}

func hasGitCommit(ctx context.Context, sh *shell.Shell, gitDir string, commit string) bool {
	// Resolve commit to an actual commit object
	output, err := sh.RunAndCapture(ctx, "git", "--git-dir", gitDir, "rev-parse", commit+"^{commit}")
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
	mirrorDir := filepath.Join(e.ExecutorConfig.GitMirrorsPath, dirForRepository(repository))
	isMainRepository := repository == e.Repository

	// Create the mirrors path if it doesn't exist
	if baseDir := filepath.Dir(mirrorDir); !utils.FileExists(baseDir) {
		e.shell.Commentf("Creating \"%s\"", baseDir)
		// Actual file permissions will be reduced by umask, and won't be 0777 unless the user has manually changed the umask to 000
		if err := os.MkdirAll(baseDir, 0777); err != nil {
			return "", err
		}
	}

	e.shell.Chdir(e.ExecutorConfig.GitMirrorsPath)

	lockTimeout := time.Second * time.Duration(e.GitMirrorsLockTimeout)

	if e.Debug {
		e.shell.Commentf("Acquiring mirror repository clone lock")
	}

	// Lock the mirror dir to prevent concurrent clones
	cloneCtx, canc := context.WithTimeout(ctx, lockTimeout)
	defer canc()
	mirrorCloneLock, err := e.shell.LockFile(cloneCtx, mirrorDir+".clonelock")
	if err != nil {
		return "", err
	}
	defer mirrorCloneLock.Unlock()

	// If we don't have a mirror, we need to clone it
	if !utils.FileExists(mirrorDir) {
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

	// If it exists, immediately release the clone lock
	mirrorCloneLock.Unlock()

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
		return "", err
	}
	defer mirrorUpdateLock.Unlock()

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
		if e.PullRequest != "false" && strings.Contains(e.PipelineProvider, "github") {
			e.shell.Commentf("Fetch and mirror pull request head from GitHub")
			refspec := fmt.Sprintf("refs/pull/%s/head", e.PullRequest)
			// Fetch the PR head from the upstream repository into the mirror.
			if err := e.shell.Run(ctx, "git", "--git-dir", mirrorDir, "fetch", "origin", refspec); err != nil {
				return "", err
			}
		} else {
			// Fetch the build branch from the upstream repository into the mirror.
			if err := e.shell.Run(ctx, "git", "--git-dir", mirrorDir, "fetch", "origin", e.Branch); err != nil {
				return "", err
			}
		}
	} else { // not the main repo.

		// This is a mirror of a submodule.
		// Update without specifying particular ref, since we don't know which
		// ref is needed for the main build.
		// (If it doesn't contain the needed ref, then the build would fail on
		// a clean host or with a clean checkout.)
		// TODO: Investigate getting the ref from the main repo and passing
		// that in here.
		if err := e.shell.Run(ctx, "git", "--git-dir", mirrorDir, "fetch", "origin"); err != nil {
			return "", err
		}
	}

	if urlChanged {
		// Let's opportunistically fsck and gc.
		// 1. In case of remote URL confusion (bug introduced in #1959), and
		// 2. There's possibly some object churn when remotes are renamed.
		if err := e.shell.Run(ctx, "git", "--git-dir", mirrorDir, "fsck"); err != nil {
			e.shell.Logger.Warningf("Couldn't run git fsck: %v", err)
		}
		if err := e.shell.Run(ctx, "git", "--git-dir", mirrorDir, "gc"); err != nil {
			e.shell.Logger.Warningf("Couldn't run git gc: %v", err)
		}
	}

	return mirrorDir, nil
}

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
	gotURL, err := e.shell.RunAndCapture(ctx, "git", args...)
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
	return true, e.shell.Run(ctx, "git", args...)
}

func (e *Executor) getOrUpdateMirrorDir(ctx context.Context, repository string) (string, error) {
	var mirrorDir string
	// Skip updating the Git mirror before using it?
	if e.ExecutorConfig.GitMirrorsSkipUpdate {
		mirrorDir = filepath.Join(e.ExecutorConfig.GitMirrorsPath, dirForRepository(repository))
		e.shell.Commentf("Skipping update and using existing mirror for repository %s at %s.", repository, mirrorDir)

		// Check if specified mirrorDir exists, otherwise the clone will fail.
		if !utils.FileExists(mirrorDir) {
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
	span, _ := tracetools.StartSpanFromContext(ctx, "repo-checkout", e.ExecutorConfig.TracingBackend)
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
	if e.ExecutorConfig.GitMirrorsPath != "" && e.ExecutorConfig.Repository != "" {
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
	if utils.FileExists(existingGitDir) {
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
		if err := gitFetch(ctx, e.shell, gitFetchFlags, "origin", e.RefSpec); err != nil {
			return fmt.Errorf("fetching refspec %q: %w", e.RefSpec, err)
		}

	case e.PullRequest != "false" && strings.Contains(e.PipelineProvider, "github"):
		// GitHub has a special ref which lets us fetch a pull request head, whether
		// or not it's a current head in this repository or a fork. See:
		// https://help.github.com/articles/checking-out-pull-requests-locally/#modifying-an-inactive-pull-request-locally
		e.shell.Commentf("Fetch and checkout pull request head from GitHub")
		refspec := fmt.Sprintf("refs/pull/%s/head", e.PullRequest)

		if err := gitFetch(ctx, e.shell, gitFetchFlags, "origin", refspec); err != nil {
			return fmt.Errorf("fetching PR refspec %q: %w", refspec, err)
		}

		gitFetchHead, _ := e.shell.RunAndCapture(ctx, "git", "rev-parse", "FETCH_HEAD")
		e.shell.Commentf("FETCH_HEAD is now `%s`", gitFetchHead)

		if e.Commit != "HEAD" {
			// If we know the commit, also fetch it directly. The commit might not be in the history of `refspec` if there
			// have been force pushes to the pull request, so this ensures we have it.
			if err := gitFetchCommitWithFallback(ctx, e.shell, gitFetchFlags, e.Commit); err != nil {
				return err
			}
		}

	case e.Commit == "HEAD":
		// If the commit is "HEAD" then we can't do a commit-specific fetch and will
		// need to fetch the remote head and checkout the fetched head explicitly.
		e.shell.Commentf("Fetch and checkout remote branch HEAD commit")
		if err := gitFetch(ctx, e.shell, gitFetchFlags, "origin", e.Branch); err != nil {
			return fmt.Errorf("fetching branch %q: %w", e.Branch, err)
		}

	default:
		// Otherwise fetch and checkout the commit directly.
		if err := gitFetchCommitWithFallback(ctx, e.shell, gitFetchFlags, e.Commit); err != nil {
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
			e.shell.OptionalWarningf("submodules-disabled", "This repository has submodules, but submodules are disabled at an agent level")
		}
	}

	if gitSubmodules {
		// `submodule sync` will ensure the .git/config
		// matches the .gitmodules file.  The command
		// is only available in git version 1.8.1, so
		// if the call fails, continue the job
		// script, and show an informative error.
		if err := e.shell.Run(ctx, "git", "submodule", "sync", "--recursive"); err != nil {
			gitVersionOutput, _ := e.shell.RunAndCapture(ctx, "git", "--version")
			e.shell.Warningf("Failed to recursively sync git submodules. This is most likely because you have an older version of git installed (" + gitVersionOutput + ") and you need version 1.8.1 and above. If you're using submodules, it's highly recommended you upgrade if you can.")
		}

		args := []string{}
		for _, config := range e.GitSubmoduleCloneConfig {
			args = append(args, "-c", config)
		}

		// Checking for submodule repositories
		submoduleRepos, err := gitEnumerateSubmoduleURLs(ctx, e.shell)
		if err != nil {
			e.shell.Warningf("Failed to enumerate git submodules: %v", err)
		} else {
			mirrorSubmodules := e.ExecutorConfig.GitMirrorsPath != ""
			submoduleMirrorDirs := make([]string, 0)
			for _, repository := range submoduleRepos {
				submoduleArgs := append([]string(nil), args...)
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

				submoduleMirrorDirs = append(submoduleMirrorDirs, mirrorDir)

				// Switch back to the checkout dir, doing other operations from GitMirrorsPath will fail.
				if err := e.createCheckoutDir(); err != nil {
					return fmt.Errorf("creating checkout dir: %w", err)
				}

				// Tests use a local temp path for the repository, real repositories don't. Handle both.
				var repositoryPath string
				if !utils.FileExists(repository) {
					repositoryPath = filepath.Join(e.ExecutorConfig.GitMirrorsPath, dirForRepository(repository))
				} else {
					repositoryPath = repository
				}

				if mirrorDir != "" {
					submoduleArgs = append(submoduleArgs, "submodule", "update", "--init", "--recursive", "--force", "--reference", repositoryPath)
				} else {
					// Fall back to a clean update, rather than failing the checkout and therefore the build
					submoduleArgs = append(submoduleArgs, "submodule", "update", "--init", "--recursive", "--force")
				}

				if err := e.shell.Run(ctx, "git", submoduleArgs...); err != nil {
					return fmt.Errorf("updating submodules: %w", err)
				}
			}

			for i, dir := range submoduleMirrorDirs {
				e.shell.Env.Set(fmt.Sprintf("BUILDKITE_REPO_SUBMODULE_MIRROR_%d", i), dir)
			}

			if !mirrorSubmodules {
				args = append(args, "submodule", "update", "--init", "--recursive", "--force")
				if err := e.shell.Run(ctx, "git", args...); err != nil {
					return fmt.Errorf("updating submodules: %w", err)
				}
			}

			if err := e.shell.Run(ctx, "git", "submodule", "foreach", "--recursive", "git reset --hard"); err != nil {
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

func gitFetchCommitWithFallback(ctx context.Context, shell *shell.Shell, gitFetchFlags, commit string) error {
	err := gitFetch(ctx, shell, gitFetchFlags, "origin", commit)
	if err == nil {
		return nil // it worked
	}
	if gerr := new(gitError); errors.As(err, &gerr) {
		// if we fail in a way that means the repository is corrupt, we should bail
		switch gerr.Type {
		case gitErrorFetchRetryClean, gitErrorFetchBadObject:
			return fmt.Errorf("fetching commit %q: %w", commit, err)
		case gitErrorFetchBadReference:
			// fallback to fetching all heads and tags
		}
	}

	// The commit might be something that's not possible to fetch directly
	// (eg. a short commit hash), so we fall back to fetching all heads and tags,
	// hoping that the commit is included.
	shell.Commentf("Commit fetch failed, trying to fetch all heads and tags")
	// By default `git fetch origin` will only fetch tags which are
	// reachable from a fetches branch. git 1.9.0+ changed `--tags` to
	// fetch all tags in addition to the default refspec, but pre 1.9.0 it
	// excludes the default refspec.
	gitFetchRefspec, err := shell.RunAndCapture(ctx, "git", "config", "remote.origin.fetch")
	if err != nil {
		return fmt.Errorf("getting remote.origin.fetch: %w", err)
	}

	if err := gitFetch(ctx, shell,
		gitFetchFlags, "origin", gitFetchRefspec, "+refs/tags/*:refs/tags/*",
	); err != nil {
		return fmt.Errorf("fetching commit %q: %w", commit, err)
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
	if err := e.shell.Run(ctx, "buildkite-agent", "meta-data", "exists", CommitMetadataKey); err == nil {
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
		"HEAD",
		"-s", // --no-patch was introduced in v1.8.4 in 2013, but e.g. CentOS 7 isn't there yet
		"--no-color",
		"--format=commit %H%nabbrev-commit %h%nAuthor: %an <%ae>%n%n%w(0,4,4)%B",
	}
	out, err := e.shell.RunAndCapture(ctx, "git", gitArgs...)
	if err != nil {
		return fmt.Errorf("getting git commit information: %w", err)
	}

	stdin := strings.NewReader(out)
	if err := e.shell.WithStdin(stdin).Run(ctx, "buildkite-agent", "meta-data", "set", CommitMetadataKey); err != nil {
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
	cmdOut, err := e.shell.RunAndCapture(ctx, "git", "rev-parse", commitRef)
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
