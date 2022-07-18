package bootstrap

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/buildkite/agent/v3/bootstrap/shell"
	"github.com/buildkite/agent/v3/experiments"
	"github.com/buildkite/agent/v3/tracetools"
	"github.com/buildkite/agent/v3/utils"
	"github.com/buildkite/roko"
	"github.com/pkg/errors"
)

func dirForRepository(repository string) string {
	badCharsPattern := regexp.MustCompile("[[:^alnum:]]")
	return badCharsPattern.ReplaceAllString(repository, "-")
}

// Given a repository, it will add the host to the set of SSH known_hosts on the machine
func addRepositoryHostToSSHKnownHosts(sh *shell.Shell, repository string) {
	if utils.FileExists(repository) {
		return
	}

	knownHosts, err := findKnownHosts(sh)
	if err != nil {
		sh.Warningf("Failed to find SSH known_hosts file: %v", err)
		return
	}

	if err = knownHosts.AddFromRepository(repository); err != nil {
		sh.Warningf("Error adding to known_hosts: %v", err)
		return
	}
}

func (b *Bootstrap) removeCheckoutDir() error {
	checkoutPath, _ := b.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH")

	// on windows, sometimes removing large dirs can fail for various reasons
	// for instance having files open
	// see https://github.com/golang/go/issues/20841
	for i := 0; i < 10; i++ {
		b.shell.Commentf("Removing %s", checkoutPath)
		if err := os.RemoveAll(checkoutPath); err != nil {
			b.shell.Errorf("Failed to remove \"%s\" (%s)", checkoutPath, err)
		} else {
			if _, err := os.Stat(checkoutPath); os.IsNotExist(err) {
				return nil
			} else {
				b.shell.Errorf("Failed to remove %s", checkoutPath)
			}
		}
		b.shell.Commentf("Waiting 10 seconds")
		<-time.After(time.Second * 10)
	}

	return fmt.Errorf("Failed to remove %s", checkoutPath)
}

func (b *Bootstrap) createCheckoutDir() error {
	checkoutPath, _ := b.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH")

	if !utils.FileExists(checkoutPath) {
		b.shell.Commentf("Creating \"%s\"", checkoutPath)
		// Actual file permissions will be reduced by umask, and won't be 0777 unless the user has manually changed the umask to 000
		if err := os.MkdirAll(checkoutPath, 0777); err != nil {
			return err
		}
	}

	if b.shell.Getwd() != checkoutPath {
		if err := b.shell.Chdir(checkoutPath); err != nil {
			return err
		}
	}

	return nil
}

// CheckoutPhase creates the build directory and makes sure we're running the
// build at the right commit.
func (b *Bootstrap) CheckoutPhase(ctx context.Context) error {
	span, ctx := tracetools.StartSpanFromContext(ctx, "checkout", b.Config.TracingBackend)
	var err error
	defer func() { span.FinishWithError(err) }()

	if err = b.executeGlobalHook(ctx, "pre-checkout"); err != nil {
		return err
	}

	if err = b.executePluginHook(ctx, "pre-checkout", b.pluginCheckouts); err != nil {
		return err
	}

	// Remove the checkout directory if BUILDKITE_CLEAN_CHECKOUT is present
	if b.CleanCheckout {
		b.shell.Headerf("Cleaning pipeline checkout")
		if err = b.removeCheckoutDir(); err != nil {
			return err
		}
	}

	b.shell.Headerf("Preparing working directory")

	// If we have a blank repository then use a temp dir for builds
	if b.Config.Repository == "" {
		var buildDir string
		buildDir, err = ioutil.TempDir("", "buildkite-job-"+b.Config.JobID)
		if err != nil {
			return err
		}
		b.shell.Env.Set(`BUILDKITE_BUILD_CHECKOUT_PATH`, buildDir)

		// Track the directory so we can remove it at the end of the bootstrap
		b.cleanupDirs = append(b.cleanupDirs, buildDir)
	}

	// Make sure the build directory exists
	if err = b.createCheckoutDir(); err != nil {
		return err
	}

	// There can only be one checkout hook, either plugin or global, in that order
	switch {
	case b.hasPluginHook("checkout"):
		if err = b.executePluginHook(ctx, "checkout", b.pluginCheckouts); err != nil {
			return err
		}
	case b.hasGlobalHook("checkout"):
		if err = b.executeGlobalHook(ctx, "checkout"); err != nil {
			return err
		}
	default:
		if b.Config.Repository != "" {
			err = roko.NewRetrier(
				roko.WithMaxAttempts(3),
				roko.WithStrategy(roko.Constant(2*time.Second)),
			).Do(func(r *roko.Retrier) error {
				err := b.defaultCheckoutPhase(ctx)
				if err == nil {
					return nil
				}

				switch {
				case shell.IsExitError(err) && shell.GetExitCode(err) == -1:
					b.shell.Warningf("Checkout was interrupted by a signal")
					r.Break()

				case errors.Cause(err) == context.Canceled:
					b.shell.Warningf("Checkout was cancelled")
					r.Break()

				default:
					b.shell.Warningf("Checkout failed! %s (%s)", err, r)

					// Specifically handle git errors
					if ge, ok := err.(*gitError); ok {
						switch ge.Type {
						// These types can fail because of corrupted checkouts
						case gitErrorClone:
						case gitErrorClean:
						case gitErrorCleanSubmodules:
							// do nothing, this will fall through to destroy the checkout

						default:
							return err
						}
					}

					// Checkout can fail because of corrupted files in the checkout
					// which can leave the agent in a state where it keeps failing
					// This removes the checkout dir, which means the next checkout
					// will be a lot slower (clone vs fetch), but hopefully will
					// allow the agent to self-heal
					_ = b.removeCheckoutDir()

					// Now make sure the build directory exists again before we try
					// to checkout again, or proceed and run hooks which presume the
					// checkout dir exists
					if err := b.createCheckoutDir(); err != nil {
						return err
					}

				}

				return err
			})
			if err != nil {
				return err
			}
		} else {
			b.shell.Commentf("Skipping checkout, BUILDKITE_REPO is empty")
		}
	}

	// Store the current value of BUILDKITE_BUILD_CHECKOUT_PATH, so we can detect if
	// one of the post-checkout hooks changed it.
	previousCheckoutPath, _ := b.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH")

	// Run post-checkout hooks
	if err = b.executeGlobalHook(ctx, "post-checkout"); err != nil {
		return err
	}

	if err = b.executeLocalHook(ctx, "post-checkout"); err != nil {
		return err
	}

	if err = b.executePluginHook(ctx, "post-checkout", b.pluginCheckouts); err != nil {
		return err
	}

	// Capture the new checkout path so we can see if it's changed.
	newCheckoutPath, _ := b.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH")

	// If the working directory has been changed by a hook, log and switch to it
	if previousCheckoutPath != "" && previousCheckoutPath != newCheckoutPath {
		b.shell.Headerf("A post-checkout hook has changed the working directory to \"%s\"", newCheckoutPath)

		if err = b.shell.Chdir(newCheckoutPath); err != nil {
			return err
		}
	}

	return nil
}

func hasGitSubmodules(sh *shell.Shell) bool {
	return utils.FileExists(filepath.Join(sh.Getwd(), ".gitmodules"))
}

func hasGitCommit(sh *shell.Shell, gitDir string, commit string) bool {
	// Resolve commit to an actual commit object
	output, err := sh.RunAndCapture("git", "--git-dir", gitDir, "rev-parse", commit+"^{commit}")
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

func (b *Bootstrap) updateGitMirror() (string, error) {
	// Create a unique directory for the repository mirror
	mirrorDir := filepath.Join(b.Config.GitMirrorsPath, dirForRepository(b.Repository))

	// Create the mirrors path if it doesn't exist
	if baseDir := filepath.Dir(mirrorDir); !utils.FileExists(baseDir) {
		b.shell.Commentf("Creating \"%s\"", baseDir)
		// Actual file permissions will be reduced by umask, and won't be 0777 unless the user has manually changed the umask to 000
		if err := os.MkdirAll(baseDir, 0777); err != nil {
			return "", err
		}
	}

	b.shell.Chdir(b.Config.GitMirrorsPath)

	lockTimeout := time.Second * time.Duration(b.GitMirrorsLockTimeout)

	if b.Debug {
		b.shell.Commentf("Acquiring mirror repository clone lock")
	}

	// Lock the mirror dir to prevent concurrent clones
	mirrorCloneLock, err := b.shell.LockFile(mirrorDir+".clonelock", lockTimeout)
	if err != nil {
		return "", err
	}
	defer mirrorCloneLock.Unlock()

	// If we don't have a mirror, we need to clone it
	if !utils.FileExists(mirrorDir) {
		b.shell.Commentf("Cloning a mirror of the repository to %q", mirrorDir)
		flags := "--mirror " + b.GitCloneMirrorFlags
		if err := gitClone(b.shell, flags, b.Repository, mirrorDir); err != nil {
			b.shell.Commentf("Removing mirror dir %q due to failed clone", mirrorDir)
			if err := os.RemoveAll(mirrorDir); err != nil {
				b.shell.Errorf("Failed to remove \"%s\" (%s)", mirrorDir, err)
			}
			return "", err
		}

		return mirrorDir, nil
	}

	// If it exists, immediately release the clone lock
	mirrorCloneLock.Unlock()

	// Check if the mirror has a commit, this is atomic so should be safe to do
	if hasGitCommit(b.shell, mirrorDir, b.Commit) {
		b.shell.Commentf("Commit %q exists in mirror", b.Commit)
		return mirrorDir, nil
	}

	if b.Debug {
		b.shell.Commentf("Acquiring mirror repository update lock")
	}

	// Lock the mirror dir to prevent concurrent updates
	mirrorUpdateLock, err := b.shell.LockFile(mirrorDir+".updatelock", lockTimeout)
	if err != nil {
		return "", err
	}
	defer mirrorUpdateLock.Unlock()

	// Check again after we get a lock, in case the other process has already updated
	if hasGitCommit(b.shell, mirrorDir, b.Commit) {
		b.shell.Commentf("Commit %q exists in mirror", b.Commit)
		return mirrorDir, nil
	}

	b.shell.Commentf("Updating existing repository mirror to find commit %s", b.Commit)

	// Update the origin of the repository so we can gracefully handle repository renames
	if err := b.shell.Run("git", "--git-dir", mirrorDir, "remote", "set-url", "origin", b.Repository); err != nil {
		return "", err
	}

	if b.PullRequest != "false" && strings.Contains(b.PipelineProvider, "github") {
		b.shell.Commentf("Fetch and mirror pull request head from GitHub")
		refspec := fmt.Sprintf("refs/pull/%s/head", b.PullRequest)
		// Fetch the PR head from the upstream repository into the mirror.
		if err := b.shell.Run("git", "--git-dir", mirrorDir, "fetch", "origin", refspec); err != nil {
			return "", err
		}
	} else {
		// Fetch the build branch from the upstream repository into the mirror.
		if err := b.shell.Run("git", "--git-dir", mirrorDir, "fetch", "origin", b.Branch); err != nil {
			return "", err
		}
	}

	return mirrorDir, nil
}

// defaultCheckoutPhase is called by the CheckoutPhase if no global or plugin checkout
// hook exists. It performs the default checkout on the Repository provided in the config
func (b *Bootstrap) defaultCheckoutPhase(ctx context.Context) error {
	span, _ := tracetools.StartSpanFromContext(ctx, "repo-checkout", b.Config.TracingBackend)
	span.AddAttributes(map[string]string{
		"checkout.repo_name": b.Repository,
		"checkout.refspec":   b.RefSpec,
		"checkout.commit":    b.Commit,
	})
	var err error
	defer func() { span.FinishWithError(err) }()

	if b.SSHKeyscan {
		addRepositoryHostToSSHKnownHosts(b.shell, b.Repository)
	}

	var mirrorDir string

	// If we can, get a mirror of the git repository to use for reference later
	if experiments.IsEnabled(`git-mirrors`) && b.Config.GitMirrorsPath != "" && b.Config.Repository != "" {
		b.shell.Commentf("Using git-mirrors experiment 🧪")
		span.AddAttributes(map[string]string{"checkout.is_using_git_mirrors": "true"})

		// Skip updating the Git mirror before using it?
		if b.Config.GitMirrorsSkipUpdate {
			mirrorDir = filepath.Join(b.Config.GitMirrorsPath, dirForRepository(b.Repository))
			b.shell.Commentf("Skipping update and using existing mirror for repository %s at %s.", b.Repository, mirrorDir)

			// Check if specified mirrorDir exists, otherwise the clone will fail.
			if !utils.FileExists(mirrorDir) {
				// Fall back to a clean clone, rather than failing the clone and therefore the build
				b.shell.Commentf("No existing mirror found for repository %s at %s.", b.Repository, mirrorDir)
				mirrorDir = ""
			}
		} else {
			mirrorDir, err = b.updateGitMirror()
			if err != nil {
				return err
			}
		}

		b.shell.Env.Set("BUILDKITE_REPO_MIRROR", mirrorDir)
	}

	// Make sure the build directory exists and that we change directory into it
	if err := b.createCheckoutDir(); err != nil {
		return err
	}

	gitCloneFlags := b.GitCloneFlags
	if mirrorDir != "" {
		gitCloneFlags += fmt.Sprintf(" --reference %q", mirrorDir)
	}

	// Does the git directory exist?
	existingGitDir := filepath.Join(b.shell.Getwd(), ".git")
	if utils.FileExists(existingGitDir) {
		// Update the origin of the repository so we can gracefully handle repository renames
		if err := b.shell.Run("git", "remote", "set-url", "origin", b.Repository); err != nil {
			return err
		}
	} else {
		if err := gitClone(b.shell, gitCloneFlags, b.Repository, "."); err != nil {
			return err
		}
	}

	// Git clean prior to checkout, we do this even if submodules have been
	// disabled to ensure previous submodules are cleaned up
	if hasGitSubmodules(b.shell) {
		if err := gitCleanSubmodules(b.shell, b.GitCleanFlags); err != nil {
			return err
		}
	}

	if err := gitClean(b.shell, b.GitCleanFlags); err != nil {
		return err
	}

	gitFetchFlags := b.GitFetchFlags

	// If a refspec is provided then use it instead.
	// For example, `refs/not/a/head`
	if b.RefSpec != "" {
		b.shell.Commentf("Fetch and checkout custom refspec")
		if err := gitFetch(b.shell, gitFetchFlags, "origin", b.RefSpec); err != nil {
			return err
		}

		// GitHub has a special ref which lets us fetch a pull request head, whether
		// or not there is a current head in this repository or another which
		// references the commit. We presume a commit sha is provided. See:
		// https://help.github.com/articles/checking-out-pull-requests-locally/#modifying-an-inactive-pull-request-locally
	} else if b.PullRequest != "false" && strings.Contains(b.PipelineProvider, "github") {
		b.shell.Commentf("Fetch and checkout pull request head from GitHub")
		refspec := fmt.Sprintf("refs/pull/%s/head", b.PullRequest)

		if err := gitFetch(b.shell, gitFetchFlags, "origin", refspec); err != nil {
			return err
		}

		gitFetchHead, _ := b.shell.RunAndCapture("git", "rev-parse", "FETCH_HEAD")
		b.shell.Commentf("FETCH_HEAD is now `%s`", gitFetchHead)

		// If the commit is "HEAD" then we can't do a commit-specific fetch and will
		// need to fetch the remote head and checkout the fetched head explicitly.
	} else if b.Commit == "HEAD" {
		b.shell.Commentf("Fetch and checkout remote branch HEAD commit")
		if err := gitFetch(b.shell, gitFetchFlags, "origin", b.Branch); err != nil {
			return err
		}

		// Otherwise fetch and checkout the commit directly. Some repositories don't
		// support fetching a specific commit so we fall back to fetching all heads
		// and tags, hoping that the commit is included.
	} else {
		if err := gitFetch(b.shell, gitFetchFlags, "origin", b.Commit); err != nil {
			// By default `git fetch origin` will only fetch tags which are
			// reachable from a fetches branch. git 1.9.0+ changed `--tags` to
			// fetch all tags in addition to the default refspec, but pre 1.9.0 it
			// excludes the default refspec.
			gitFetchRefspec, _ := b.shell.RunAndCapture("git", "config", "remote.origin.fetch")
			if err := gitFetch(b.shell, gitFetchFlags, "origin", gitFetchRefspec, "+refs/tags/*:refs/tags/*"); err != nil {
				return err
			}
		}
	}

	if b.Commit == "HEAD" {
		if err := gitCheckout(b.shell, "-f", "FETCH_HEAD"); err != nil {
			return err
		}
	} else {
		if err := gitCheckout(b.shell, "-f", b.Commit); err != nil {
			return err
		}
	}

	var gitSubmodules bool
	if !b.GitSubmodules && hasGitSubmodules(b.shell) {
		b.shell.Warningf("This repository has submodules, but submodules are disabled at an agent level")
	} else if b.GitSubmodules && hasGitSubmodules(b.shell) {
		b.shell.Commentf("Git submodules detected")
		gitSubmodules = true
	}

	if gitSubmodules {
		// `submodule sync` will ensure the .git/config
		// matches the .gitmodules file.  The command
		// is only available in git version 1.8.1, so
		// if the call fails, continue the bootstrap
		// script, and show an informative error.
		if err := b.shell.Run("git", "submodule", "sync", "--recursive"); err != nil {
			gitVersionOutput, _ := b.shell.RunAndCapture("git", "--version")
			b.shell.Warningf("Failed to recursively sync git submodules. This is most likely because you have an older version of git installed (" + gitVersionOutput + ") and you need version 1.8.1 and above. If you're using submodules, it's highly recommended you upgrade if you can.")
		}

		// Checking for submodule repositories
		submoduleRepos, err := gitEnumerateSubmoduleURLs(b.shell)
		if err != nil {
			b.shell.Warningf("Failed to enumerate git submodules: %v", err)
		} else {
			for _, repository := range submoduleRepos {
				// submodules might need their fingerprints verified too
				if b.SSHKeyscan {
					addRepositoryHostToSSHKnownHosts(b.shell, repository)
				}
			}
		}

		if err := b.shell.Run("git", "submodule", "update", "--init", "--recursive", "--force"); err != nil {
			return err
		}

		if err := b.shell.Run("git", "submodule", "foreach", "--recursive", "git reset --hard"); err != nil {
			return err
		}
	}

	// Git clean after checkout. We need to do this because submodules could have
	// changed in between the last checkout and this one. A double clean is the only
	// good solution to this problem that we've found
	b.shell.Commentf("Cleaning again to catch any post-checkout changes")

	if err := gitClean(b.shell, b.GitCleanFlags); err != nil {
		return err
	}

	if gitSubmodules {
		if err := gitCleanSubmodules(b.shell, b.GitCleanFlags); err != nil {
			return err
		}
	}

	if _, hasToken := b.shell.Env.Get("BUILDKITE_AGENT_ACCESS_TOKEN"); !hasToken {
		b.shell.Warningf("Skipping sending Git information to Buildkite as $BUILDKITE_AGENT_ACCESS_TOKEN is missing")
		return nil
	}

	// resolve BUILDKITE_COMMIT based on the local git repo
	if experiments.IsEnabled(`resolve-commit-after-checkout`) {
		b.shell.Commentf("Using resolve-commit-after-checkout experiment 🧪")
		b.resolveCommit()
	}

	// Grab author and commit information and send
	// it back to Buildkite. But before we do,
	// we'll check to see if someone else has done
	// it first.
	b.shell.Commentf("Checking to see if Git data needs to be sent to Buildkite")
	if err := b.shell.Run("buildkite-agent", "meta-data", "exists", "buildkite:git:commit"); err != nil {
		b.shell.Commentf("Sending Git commit information back to Buildkite")
		out, err := b.shell.RunAndCapture("git", "--no-pager", "show", "HEAD", "-s", "--format=fuller", "--no-color", "--")
		if err != nil {
			return err
		}
		stdin := strings.NewReader(out)
		if err := b.shell.WithStdin(stdin).Run("buildkite-agent", "meta-data", "set", "buildkite:git:commit"); err != nil {
			return err
		}
	}

	return nil
}

func (b *Bootstrap) resolveCommit() {
	commitRef, _ := b.shell.Env.Get("BUILDKITE_COMMIT")
	if commitRef == "" {
		b.shell.Warningf("BUILDKITE_COMMIT was empty")
		return
	}
	cmdOut, err := b.shell.RunAndCapture(`git`, `rev-parse`, commitRef)
	if err != nil {
		b.shell.Warningf("Error running git rev-parse %q: %v", commitRef, err)
		return
	}
	trimmedCmdOut := strings.TrimSpace(string(cmdOut))
	if trimmedCmdOut != commitRef {
		b.shell.Commentf("Updating BUILDKITE_COMMIT from %q to %q", commitRef, trimmedCmdOut)
		b.shell.Env.Set(`BUILDKITE_COMMIT`, trimmedCmdOut)
	}
}
