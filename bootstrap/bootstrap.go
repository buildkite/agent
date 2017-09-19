package bootstrap

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/buildkite/agent/agent"
	"github.com/buildkite/agent/bootstrap/shell"
	"github.com/buildkite/agent/env"
	"github.com/pkg/errors"
)

// Bootstrap represents the phases of execution in a Buildkite Job. It's run
// as a sub-process of the buildkite-agent and finishes at the conclusion of a job.
// Historically (prior to v3) the bootstrap was a shell script, but was ported to
// Golang for portability and testability
type Bootstrap struct {
	// Config provides the bootstrap configuration
	Config

	// Shell is the shell environment for the bootstrap
	shell *shell.Shell

	// Plugins are the plugins that are created in the PluginPhase
	plugins []*agent.Plugin

	// Tracks whether there is a checkout to upload in the teardown
	hasCheckout bool
}

// Start runs the bootstrap and returns the exit code
func (b *Bootstrap) Start() int {
	// Check if not nil to allow for tests to overwrite shell
	if b.shell == nil {
		var err error
		b.shell, err = shell.New()
		if err != nil {
			fmt.Printf("Error creating shell: %v", err)
			return 1
		}

		// Apply PTY settings
		b.shell.PTY = b.Config.RunInPty
	}

	// Tear down the environment (and fire pre-exit hook) before we exit
	defer func() {
		if err := b.tearDown(); err != nil {
			b.shell.Errorf("Error tearing down bootstrap: %v", err)
		}
	}()

	// Initialize the environment, a failure here will still call the tearDown
	if err := b.setUp(); err != nil {
		b.shell.Errorf("Error setting up bootstrap: %v", err)
		return 1
	}

	// These are the "Phases of bootstrap execution". They are designed to be
	// run independently at some later stage (think buildkite-agent bootstrap checkout)
	var phases = []func() error{
		b.PluginPhase,
		b.CheckoutPhase,
		b.CommandPhase,
	}

	var phaseError error

	for _, phase := range phases {
		if phaseError = phase(); phaseError != nil {
			break
		}
	}

	if err := b.uploadArtifacts(); err != nil {
		b.shell.Errorf("%v", err)
		return shell.GetExitCode(err)
	}

	// Phase errors are where something of ours broke that merits a big red error
	// this won't include command failures, as we view that as more in the user space
	if phaseError != nil {
		b.shell.Errorf("%v", phaseError)
		return shell.GetExitCode(phaseError)
	}

	// Use the exit code from the command phase
	exitStatus, _ := strconv.Atoi(b.shell.Env.Get(`BUILDKITE_COMMAND_EXIT_STATUS`))
	return exitStatus
}

// executeHook runs a hook script with the hookRunner
func (b *Bootstrap) executeHook(name string, hookPath string, extraEnviron *env.Environment) error {
	b.shell.Headerf("Running %s hook", name)
	if !fileExists(hookPath) {
		if b.Debug {
			b.shell.Commentf("Skipping, no hook script found at \"%s\"", hookPath)
		}
		return nil
	}

	// We need a script to wrap the hook script so that we can snaffle the changed
	// environment variables
	script, err := newHookScriptWrapper(hookPath)
	if err != nil {
		b.shell.Errorf("Error creating hook script: %v", err)
		return err
	}
	defer script.Close()

	if b.Debug {
		b.shell.Commentf("A hook runner was written to \"%s\" with the following:", script.Path())
		b.shell.Printf("%s", hookPath)
	}

	b.shell.Commentf("Executing \"%s\"", script.Path())

	// Run the wrapper script
	if err := b.shell.RunScript(script.Path(), extraEnviron); err != nil {
		b.shell.Env.Set("BUILDKITE_LAST_HOOK_EXIT_STATUS", fmt.Sprintf("%d", shell.GetExitCode(err)))
		b.shell.Errorf("The %s hook exited with an error: %v", name, err)
		return err
	}

	// Store the last hook exit code for subsequent steps
	b.shell.Env.Set("BUILDKITE_LAST_HOOK_EXIT_STATUS",
		fmt.Sprintf("%d", shell.GetExitCode(err)))

	if err != nil {
		return errors.Wrapf(err, "The %s hook exited with an error", name)
	}

	// Get changed environent
	changes, err := script.ChangedEnvironment()
	if err != nil {
		return errors.Wrapf(err, "Failed to get environment")
	}

	// Finally, apply changes to the current shell and config
	b.applyEnvironmentChanges(changes)
	return nil
}

func (b *Bootstrap) applyEnvironmentChanges(environ *env.Environment) {
	// `environ` shouldn't ever be `nil`, but we'll just be cautious and guard against it.
	if environ == nil {
		return
	}

	// Do we even have any environment variables to change?
	if environ.Length() > 0 {
		// First, let see any of the environment variables are supposed
		// to change the bootstrap configuration at run time.
		bootstrapConfigEnvChanges := b.Config.ReadFromEnvironment(environ)

		b.shell.Headerf("Applying environment changes")

		// Print out the env vars that changed. As we go through each
		// one, we'll determine if it was a special "bootstrap"
		// environment variable that has changed the bootstrap
		// configuration at runtime.
		//
		// If it's "special", we'll show the value it was changed to -
		// otherwise we'll hide it. Since we don't know if an
		// environment variable contains sensitive information (i.e.
		// THIRD_PARTY_API_KEY) we'll just not show any values for
		// anything not controlled by us.
		for k, v := range environ.ToMap() {
			_, ok := bootstrapConfigEnvChanges[k]
			if ok {
				b.shell.Commentf("%s is now %q", k, v)
			} else {
				b.shell.Commentf("%s changed", k)
			}
		}

		// Now that we've finished telling the user what's changed,
		// let's mutate the current shell environment to include all
		// the new values.
		b.shell.Env = b.shell.Env.Merge(environ)
	}
}

// Returns the absolute path to a global hook
func (b *Bootstrap) globalHookPath(name string) string {
	return filepath.Join(b.HooksPath, normalizeScriptFileName(name))
}

// Executes a global hook
func (b *Bootstrap) executeGlobalHook(name string) error {
	return b.executeHook("global "+name, b.globalHookPath(name), nil)
}

// Returns the absolute path to a local hook
func (b *Bootstrap) localHookPath(name string) string {
	return filepath.Join(b.shell.Getwd(), ".buildkite", "hooks", normalizeScriptFileName(name))
}

// Executes a local hook
func (b *Bootstrap) executeLocalHook(name string) error {
	return b.executeHook("local "+name, b.localHookPath(name), nil)
}

// Returns the absolute path to a plugin hook
func (b *Bootstrap) pluginHookPath(plugin *agent.Plugin, name string) (string, error) {
	id, err := plugin.Identifier()
	if err != nil {
		return "", err
	}

	dir, err := plugin.RepositorySubdirectory()
	if err != nil {
		return "", err
	}

	return filepath.Join(b.PluginsPath, id, dir, "hooks", normalizeScriptFileName(name)), nil
}

// Executes a plugin hook
func (b *Bootstrap) executePluginHook(plugins []*agent.Plugin, name string) error {
	for _, p := range plugins {
		path, err := b.pluginHookPath(p, name)
		if err != nil {
			return err
		}

		env, _ := p.ConfigurationToEnvironment()
		if err := b.executeHook("plugin "+p.Label()+" "+name, path, env); err != nil {
			return err
		}
	}
	return nil
}

// If a plugin hook exists with this name
func (b *Bootstrap) pluginHookExists(plugins []*agent.Plugin, name string) bool {
	for _, p := range plugins {
		path, err := b.pluginHookPath(p, name)
		if err != nil {
			return false
		}
		if fileExists(path) {
			return true
		}
	}

	return false
}

// Returns whether or not a file exists on the filesystem. We consider any
// error returned by os.Stat to indicate that the file doesn't exist. We could
// be speciifc and use os.IsNotExist(err), but most other errors also indicate
// that the file isn't there (or isn't available) so we'll just catch them all.
func fileExists(filename string) bool {
	_, err := os.Stat(filename)
	return err == nil
}

// Returns a platform specific filename for scripts
func normalizeScriptFileName(filename string) string {
	if runtime.GOOS == "windows" {
		return filename + ".bat"
	}
	return filename
}

func dirForAgentName(agentName string) string {
	badCharsPattern := regexp.MustCompile("[[:^alnum:]]")
	return badCharsPattern.ReplaceAllString(agentName, "-")
}

// Given a repostory, it will add the host to the set of SSH known_hosts on the machine
func addRepositoryHostToSSHKnownHosts(sh *shell.Shell, repository string) {
	knownHosts, err := findKnownHosts(sh)
	if err != nil {
		sh.Warningf("Failed to find SSH known_hosts file: %v", err)
		return
	}
	defer knownHosts.Unlock()

	if err = knownHosts.AddFromRepository(repository); err != nil {
		sh.Warningf("%v", err)
	}
}

// Makes sure a file is executable
func addExecutePermissiontoFile(filename string) error {
	s, err := os.Stat(filename)
	if err != nil {
		return fmt.Errorf("Failed to retrieve file information of \"%s\" (%s)", filename, err)
	}

	if s.Mode()&0100 == 0 {
		err = os.Chmod(filename, s.Mode()|0100)
		if err != nil {
			return fmt.Errorf("Failed to mark \"%s\" as executable (%s)", filename, err)
		}
	}

	return nil
}

// setUp is run before all the phases run. It's responsible for initializing the
// bootstrap environment
func (b *Bootstrap) setUp() error {
	// Create an empty env for us to keep track of our env changes in
	b.shell.Env = env.FromSlice(os.Environ())

	// Add the $BUILDKITE_BIN_PATH to the $PATH if we've been given one
	if b.BinPath != "" {
		b.shell.Env.Set("PATH", fmt.Sprintf("%s%s%s", b.BinPath, string(os.PathListSeparator), b.shell.Env.Get("PATH")))
	}

	b.shell.Env.Set("BUILDKITE_BUILD_CHECKOUT_PATH", filepath.Join(b.BuildPath, dirForAgentName(b.AgentName), b.OrganizationSlug, b.PipelineSlug))

	if b.Debug {
		b.shell.Headerf("Build environment variables")
		for _, e := range b.shell.Env.ToSlice() {
			if strings.HasPrefix(e, "BUILDKITE") || strings.HasPrefix(e, "CI") || strings.HasPrefix(e, "PATH") {
				b.shell.Printf("%s", strings.Replace(e, "\n", "\\n", -1))
			}
		}
	}

	// Disable any interactive Git/SSH prompting
	b.shell.Env.Set("GIT_TERMINAL_PROMPT", "0")

	// It's important to do this before checking out plugins, in case you want
	// to use the global environment hook to whitelist the plugins that are
	// allowed to be used.
	return b.executeGlobalHook("environment")
}

// tearDown is called before the bootstrap exits, even on error
func (b *Bootstrap) tearDown() error {
	if err := b.executeGlobalHook("pre-exit"); err != nil {
		return err
	}

	if err := b.executeLocalHook("pre-exit"); err != nil {
		return err
	}

	if err := b.executePluginHook(b.plugins, "pre-exit"); err != nil {
		return err
	}

	// Support deprecated BUILDKITE_DOCKER* env vars
	if hasDeprecatedDockerIntegration(b.shell) {
		return tearDownDeprecatedDockerIntegration(b.shell)
	}

	return nil
}

// PluginPhase is where plugins that weren't filtered in the Environment phase are
// checked out and made available to later phases
func (b *Bootstrap) PluginPhase() error {
	if b.Plugins == "" {
		return nil
	}

	b.shell.Headerf("Setting up plugins")

	// Make sure we have a plugin path before trying to do anything
	if b.PluginsPath == "" {
		return fmt.Errorf("Can't checkout plugins without a `plugins-path`")
	}

	if b.Debug {
		b.shell.Commentf("Plugin JSON is %s", b.Plugins)
	}

	// Check if we can run plugins (disabled via --no-plugins)
	if b.Plugins != "" && !b.Config.PluginsEnabled {
		return fmt.Errorf("This agent isn't allowed to run plugins. To allow this, re-run this agent without the `--no-plugins` option.")
	}

	var err error
	b.plugins, err = agent.CreatePluginsFromJSON(b.Plugins)
	if err != nil {
		return errors.Wrap(err, "Failed to parse plugin definition")
	}

	for _, p := range b.plugins {
		// Get the identifer for the plugin
		id, err := p.Identifier()
		if err != nil {
			return err
		}

		// Create a path to the plugin
		directory := filepath.Join(b.PluginsPath, id)
		pluginGitDirectory := filepath.Join(directory, ".git")

		// Has it already been checked out?
		if !fileExists(pluginGitDirectory) {
			// Make the directory
			err = os.MkdirAll(directory, 0777)
			if err != nil {
				return err
			}

			// Try and lock this particular plugin while we check it out (we create
			// the file outside of the plugin directory so git clone doesn't have
			// a cry about the directory not being empty)
			pluginCheckoutHook, err := shell.LockFileWithTimeout(b.shell, filepath.Join(b.PluginsPath, id+".lock"), time.Minute*5)
			if err != nil {
				return err
			}

			// Once we've got the lock, we need to make sure another process didn't already
			// checkout the plugin
			if fileExists(pluginGitDirectory) {
				pluginCheckoutHook.Unlock()
				b.shell.Commentf("Plugin \"%s\" found", p.Label())
				continue
			}

			repo, err := p.Repository()
			if err != nil {
				return err
			}

			b.shell.Commentf("Plugin \"%s\" will be checked out to \"%s\"", p.Location, directory)

			if b.Debug {
				b.shell.Commentf("Checking if \"%s\" is a local repository", repo)
			}

			// Switch to the plugin directory
			previousWd := b.shell.Getwd()
			if err = b.shell.Chdir(directory); err != nil {
				return err
			}

			b.shell.Commentf("Switching to the plugin directory")

			// If it's not a local repo, and we can perform
			// SSH fingerprint verification, do so.
			if !fileExists(repo) && b.SSHFingerprintVerification {
				addRepositoryHostToSSHKnownHosts(b.shell, repo)
			}

			// Plugin clones shouldn't use custom GitCloneFlags
			if err = b.shell.Run("git", "clone", "-v", "--", repo, "."); err != nil {
				return err
			}

			// Switch to the version if we need to
			if p.Version != "" {
				b.shell.Commentf("Checking out `%s`", p.Version)
				if err = b.shell.Run("git", "checkout", "-f", p.Version); err != nil {
					return err
				}
			}

			// Switch back to the previous working directory
			if err = b.shell.Chdir(previousWd); err != nil {
				return err
			}

			// Now that we've succefully checked out the
			// plugin, we can remove the lock we have on
			// it.
			pluginCheckoutHook.Unlock()
		} else {
			b.shell.Commentf("Plugin \"%s\" found", p.Label())
		}
	}

	// Now we can run plugin environment hooks too
	return b.executePluginHook(b.plugins, "environment")
}

// CheckoutPhase creates the build directory and makes sure we're running the
// build at the right commit.
func (b *Bootstrap) CheckoutPhase() error {
	if err := b.executeGlobalHook("pre-checkout"); err != nil {
		return err
	}

	if err := b.executeLocalHook("pre-checkout"); err != nil {
		return err
	}

	if err := b.executePluginHook(b.plugins, "pre-checkout"); err != nil {
		return err
	}

	// Remove the checkout directory if BUILDKITE_CLEAN_CHECKOUT is present
	if b.CleanCheckout {
		b.shell.Headerf("Cleaning pipeline checkout")
		b.shell.Commentf("Removing %s", b.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH"))

		if err := os.RemoveAll(b.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH")); err != nil {
			return fmt.Errorf("Failed to remove \"%s\" (%s)", b.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH"), err)
		}
	}

	b.shell.Headerf("Preparing build directory")

	// Create the build directory
	if !fileExists(b.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH")) {
		b.shell.Commentf("Creating \"%s\"", b.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH"))
		os.MkdirAll(b.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH"), 0777)
	}

	// Change to the new build checkout path
	if err := b.shell.Chdir(b.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH")); err != nil {
		return err
	}

	// There can only be one checkout hook, either plugin or global, in that order
	switch {
	case b.pluginHookExists(b.plugins, "checkout"):
		if err := b.executePluginHook(b.plugins, "checkout"); err != nil {
			return err
		}
	case fileExists(b.globalHookPath("checkout")):
		if err := b.executeGlobalHook("checkout"); err != nil {
			return err
		}
	default:
		if err := b.defaultCheckoutPhase(); err != nil {
			return err
		}
	}

	// After this point, artifacts will be uploaded on failure
	b.hasCheckout = true

	// Store the current value of BUILDKITE_BUILD_CHECKOUT_PATH, so we can detect if
	// one of the post-checkout hooks changed it.
	previousCheckoutPath := b.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH")

	// Run post-checkout hooks
	if err := b.executeGlobalHook("post-checkout"); err != nil {
		return err
	}

	if err := b.executeLocalHook("post-checkout"); err != nil {
		return err
	}

	if err := b.executePluginHook(b.plugins, "post-checkout"); err != nil {
		return err
	}

	// Capture the new checkout path so we can see if it's changed.
	newCheckoutPath := b.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH")

	// If the working directory has been changed by a hook, log and switch to it
	if previousCheckoutPath != "" && previousCheckoutPath != newCheckoutPath {
		b.shell.Headerf("A post-checkout hook has changed the working directory to \"%s\"", newCheckoutPath)

		if err := b.shell.Chdir(newCheckoutPath); err != nil {
			return err
		}
	}

	return nil
}

// defaultCheckoutPhase is called by the CheckoutPhase if no global or plugin checkout
// hook exists. It performs the default checkout on the Repository provided in the config
func (b *Bootstrap) defaultCheckoutPhase() error {
	if b.SSHFingerprintVerification {
		addRepositoryHostToSSHKnownHosts(b.shell, b.Repository)
	}

	// Do we need to do a git clone?
	existingGitDir := filepath.Join(b.shell.Getwd(), ".git")
	if fileExists(existingGitDir) {
		// Update the the origin of the repository so we can gracefully handle repository renames
		if err := b.shell.Run("git", "remote", "set-url", "origin", b.Repository); err != nil {
			return err
		}
	} else {
		if err := gitClone(b.shell, b.GitCloneFlags, b.Repository, "."); err != nil {
			return err
		}
	}

	// Git clean prior to checkout
	if err := gitClean(b.shell, b.GitCleanFlags, b.GitSubmodules); err != nil {
		return err
	}

	// If a refspec is provided then use it instead.
	// i.e. `refs/not/a/head`
	if b.RefSpec != "" {
		b.shell.Commentf("Fetch and checkout custom refspec")
		if err := gitFetch(b.shell, "-v --prune", "origin", b.RefSpec); err != nil {
			return err
		}

		if err := b.shell.Run("git", "checkout", "-f", b.Commit); err != nil {
			return err
		}

		// GitHub has a special ref which lets us fetch a pull request head, whether
		// or not there is a current head in this repository or another which
		// references the commit. We presume a commit sha is provided. See:
		// https://help.github.com/articles/checking-out-pull-requests-locally/#modifying-an-inactive-pull-request-locally
	} else if b.PullRequest != "false" && strings.Contains(b.PipelineProvider, "github") {
		b.shell.Commentf("Fetch and checkout pull request head")
		refspec := fmt.Sprintf("refs/pull/%s/head", b.PullRequest)

		if err := gitFetch(b.shell, "-v", "origin", refspec); err != nil {
			return err
		}

		gitFetchHead, _ := b.shell.RunAndCapture("git", "rev-parse", "FETCH_HEAD")
		b.shell.Commentf("FETCH_HEAD is now `%s`", gitFetchHead)

		if err := b.shell.Run("git", "checkout", "-f", b.Commit); err != nil {
			return err
		}

		// If the commit is "HEAD" then we can't do a commit-specific fetch and will
		// need to fetch the remote head and checkout the fetched head explicitly.
	} else if b.Commit == "HEAD" {
		b.shell.Commentf("Fetch and checkout remote branch HEAD commit")
		if err := gitFetch(b.shell, "-v --prune", "origin", b.Branch); err != nil {
			return err
		}

		if err := b.shell.Run("git", "checkout", "-f", "FETCH_HEAD"); err != nil {
			return err
		}

		// Otherwise fetch and checkout the commit directly. Some repositories don't
		// support fetching a specific commit so we fall back to fetching all heads
		// and tags, hoping that the commit is included.
	} else {
		b.shell.Commentf("Fetch and checkout commit")
		if err := gitFetch(b.shell, "-v", "origin", b.Commit); err != nil {
			// By default `git fetch origin` will only fetch tags which are
			// reachable from a fetches branch. git 1.9.0+ changed `--tags` to
			// fetch all tags in addition to the default refspec, but pre 1.9.0 it
			// excludes the default refspec.
			gitFetchRefspec, _ := b.shell.RunAndCapture("git", "config", "remote.origin.fetch")
			if err := gitFetch(b.shell, "-v --prune", "origin", gitFetchRefspec, "+refs/tags/*:refs/tags/*"); err != nil {
				return err
			}
		}
		if err := b.shell.Run("git", "checkout", "-f", b.Commit); err != nil {
			return err
		}
	}

	if b.GitSubmodules {
		// submodules might need their fingerprints verified too
		if b.SSHFingerprintVerification {
			b.shell.Commentf("Checking to see if submodule urls need to be added to known_hosts")
			submoduleRepos, err := gitEnumerateSubmoduleURLs(b.shell)
			if err != nil {
				b.shell.Warningf("Failed to enumerate git submodules: %v", err)
			} else {
				for _, repository := range submoduleRepos {
					addRepositoryHostToSSHKnownHosts(b.shell, repository)
				}
			}
		}

		// `submodule sync` will ensure the .git/config
		// matches the .gitmodules file.  The command
		// is only available in git version 1.8.1, so
		// if the call fails, continue the bootstrap
		// script, and show an informative error.
		if err := b.shell.Run("git", "submodule", "sync", "--recursive"); err != nil {
			gitVersionOutput, _ := b.shell.RunAndCapture("git", "--version")
			b.shell.Warningf("Failed to recursively sync git submodules. This is most likely because you have an older version of git installed (" + gitVersionOutput + ") and you need version 1.8.1 and above. If you're using submodules, it's highly recommended you upgrade if you can.")
		}

		if err := b.shell.Run("git", "submodule", "update", "--init", "--recursive", "--force"); err != nil {
			return err
		}
		if err := b.shell.Run("git", "submodule", "foreach", "--recursive", "git", "reset", "--hard"); err != nil {
			return err
		}
	}

	// Git clean after checkout
	if err := gitClean(b.shell, b.GitCleanFlags, b.GitSubmodules); err != nil {
		return err
	}

	if b.shell.Env.Get("BUILDKITE_AGENT_ACCESS_TOKEN") == "" {
		b.shell.Warningf("Skipping sending Git information to Buildkite as $BUILDKITE_AGENT_ACCESS_TOKEN is missing")
		return nil
	}

	// Grab author and commit information and send
	// it back to Buildkite. But before we do,
	// we'll check to see if someone else has done
	// it first.
	b.shell.Commentf("Checking to see if Git data needs to be sent to Buildkite")
	if _, err := b.shell.RunAndCapture("buildkite-agent", "meta-data", "exists", "buildkite:git:commit"); err != nil {
		b.shell.Commentf("Sending Git commit information back to Buildkite")

		gitCommitOutput, _ := b.shell.RunAndCapture("git", "show", "HEAD", "-s", "--format=fuller", "--no-color")
		gitBranchOutput, _ := b.shell.RunAndCapture("git", "branch", "--contains", "HEAD", "--no-color")

		if err = b.shell.Run("buildkite-agent", "meta-data", "set", "buildkite:git:commit", gitCommitOutput); err != nil {
			return err
		}
		if err = b.shell.Run("buildkite-agent", "meta-data", "set", "buildkite:git:branch", gitBranchOutput); err != nil {
			return err
		}
	}

	return nil
}

// CommandPhase determines how to run the build, and then runs it
func (b *Bootstrap) CommandPhase() error {
	if err := b.executeGlobalHook("pre-command"); err != nil {
		return err
	}

	if err := b.executeLocalHook("pre-command"); err != nil {
		return err
	}

	if err := b.executePluginHook(b.plugins, "pre-command"); err != nil {
		return err
	}

	var commandExitError error

	// There can only be one command hook, so we check them in order
	// of plugin, local
	switch {
	case b.pluginHookExists(b.plugins, "command"):
		commandExitError = b.executePluginHook(b.plugins, "command")
	case fileExists(b.localHookPath("command")):
		commandExitError = b.executeLocalHook("command")
	case fileExists(b.globalHookPath("command")):
		commandExitError = b.executeGlobalHook("command")
	default:
		commandExitError = b.defaultCommandPhase()
	}

	// Expand the command header if it fails
	if commandExitError != nil {
		b.shell.Printf("^^^ +++")
	}

	// Save the command exit status to the env so hooks + plugins can access it. If there is no error
	// this will be zero. It's used to set the exit code later, so it's important
	b.shell.Env.Set("BUILDKITE_COMMAND_EXIT_STATUS", fmt.Sprintf("%d", shell.GetExitCode(commandExitError)))

	// Run post-command hooks
	if err := b.executeGlobalHook("post-command"); err != nil {
		return err
	}

	if err := b.executeLocalHook("post-command"); err != nil {
		return err
	}

	if err := b.executePluginHook(b.plugins, "post-command"); err != nil {
		return err
	}

	return nil
}

// defaultCommandPhase is executed if there is no global or plugin command hook
func (b *Bootstrap) defaultCommandPhase() error {
	// Make sure we actually have a command to run
	if b.Command == "" {
		return fmt.Errorf("No command has been defined. Please go to \"Pipeline Settings\" and configure your build step's \"Command\"")
	}

	scriptFileName := strings.Replace(b.Command, "\n", "", -1)
	pathToCommand, err := filepath.Abs(filepath.Join(b.shell.Getwd(), scriptFileName))
	commandIsScript := err == nil && fileExists(pathToCommand)

	// If the command isn't a script, then it's something we need
	// to eval. But before we even try running it, we should double
	// check that the agent is allowed to eval commands.
	if !commandIsScript && !b.CommandEval {
		b.shell.Commentf("No such file: \"%s\"", scriptFileName)
		return fmt.Errorf("This agent is not allowed to evaluate console commands. To allow this, re-run this agent without the `--no-command-eval` option, or specify a script within your repository to run instead (such as scripts/test.sh).")
	}

	// Also make sure that the script we've resolved is definitely within this
	// repository checkout and isn't elsewhere on the system.
	if commandIsScript && !b.CommandEval && !strings.HasPrefix(pathToCommand, b.shell.Getwd()+string(os.PathSeparator)) {
		b.shell.Commentf("No such file: \"%s\"", scriptFileName)
		return fmt.Errorf("This agent is only allowed to run scripts within your repository. To allow this, re-run this agent without the `--no-command-eval` option, or specify a script within your repository to run instead (such as scripts/test.sh).")
	}

	var headerLabel string
	var buildScriptPath string
	var promptDisplay string

	// Come up with the contents of the build script. While we
	// generate the script, we need to handle the case of running a
	// script vs. a command differently
	if commandIsScript {
		headerLabel = "Running build script"

		if runtime.GOOS == "windows" {
			promptDisplay = b.Command
		} else {
			// Show a prettier (more accurate version) of
			// what we're doing on Linux
			promptDisplay = "./\"" + b.Command + "\""
		}

		buildScriptPath = pathToCommand
	} else {
		headerLabel = "Running command"

		// Create a build script that will output each line of the command, and run it.
		var buildScriptContents string
		if runtime.GOOS == "windows" {
			buildScriptContents = "@echo off\n"
			for _, k := range strings.Split(b.Command, "\n") {
				if k != "" {
					buildScriptContents = buildScriptContents +
						fmt.Sprintf("ECHO %s\n", shell.BatchEscape("\033[90m>\033[0m "+k)) +
						k + "\n" +
						"if %errorlevel% neq 0 exit /b %errorlevel%\n"
				}
			}
		} else {
			buildScriptContents = "#!/bin/bash\nset -e\n"
			for _, k := range strings.Split(b.Command, "\n") {
				if k != "" {
					buildScriptContents = buildScriptContents +
						fmt.Sprintf("echo '\033[90m$\033[0m %s'\n", strings.Replace(k, "'", "'\\''", -1)) +
						k + "\n"
				}
			}
		}

		// Create a temporary file where we'll run a program from
		buildScriptPath = filepath.Join(b.shell.Getwd(), normalizeScriptFileName("buildkite-script-"+b.JobID))

		if b.Debug {
			b.shell.Headerf("Preparing build script")
			b.shell.Commentf("A build script is being written to \"%s\" with the following:", buildScriptPath)
			b.shell.Printf("%s", buildScriptContents)
		}

		// Write the build script to disk
		err := ioutil.WriteFile(buildScriptPath, []byte(buildScriptContents), 0644)
		if err != nil {
			return errors.Wrapf(err, "Failed to write to \"%s\"", buildScriptPath)
		}
	}

	// Make script executable
	if err = addExecutePermissiontoFile(buildScriptPath); err != nil {
		return err
	}

	// Show we're running the script
	b.shell.Headerf("%s", headerLabel)
	if promptDisplay != "" {
		b.shell.Promptf("%s", promptDisplay)
	}

	// Support deprecated BUILDKITE_DOCKER* env vars
	if hasDeprecatedDockerIntegration(b.shell) {
		if b.Debug {
			b.shell.Commentf("Detected deprecated docker environment variables")
		}
		return runDeprecatedDockerIntegration(b.shell, buildScriptPath)
	}

	return b.shell.RunScript(buildScriptPath, nil)
}

func (b *Bootstrap) uploadArtifacts() error {
	if !b.hasCheckout {
		b.shell.Commentf("Skipping artifact upload, no checkout")
		return nil
	}

	if b.AutomaticArtifactUploadPaths == "" {
		b.shell.Commentf("Skipping artifact upload, no artifact upload path set")
		return nil
	}

	// Run pre-artifact hooks
	if err := b.executeGlobalHook("pre-artifact"); err != nil {
		return err
	}

	if err := b.executeLocalHook("pre-artifact"); err != nil {
		return err
	}

	if err := b.executePluginHook(b.plugins, "pre-artifact"); err != nil {
		return err
	}

	// Run the artifact upload command
	b.shell.Headerf("Uploading artifacts")
	args := []string{"artifact", "upload", b.AutomaticArtifactUploadPaths}

	// If blank, the upload destination is buildkite
	if b.ArtifactUploadDestination != "" {
		b.shell.Commentf("Using default artifact upload destination")
		args = append(args, b.ArtifactUploadDestination)
	}

	if err := b.shell.Run("buildkite-agent", args...); err != nil {
		return err
	}

	// Run post-artifact hooks
	if err := b.executeGlobalHook("post-artifact"); err != nil {
		return err
	}

	if err := b.executeLocalHook("post-artifact"); err != nil {
		return err
	}

	if err := b.executePluginHook(b.plugins, "post-artifact"); err != nil {
		return err
	}

	return nil
}
