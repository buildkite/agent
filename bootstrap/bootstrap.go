package bootstrap

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/buildkite/agent/agent/plugin"
	"github.com/buildkite/agent/bootstrap/shell"
	"github.com/buildkite/agent/env"
	"github.com/buildkite/agent/process"
	"github.com/buildkite/agent/retry"
	"github.com/buildkite/shellwords"
	"github.com/pkg/errors"
)

// Bootstrap represents the phases of execution in a Buildkite Job. It's run
// as a sub-process of the buildkite-agent and finishes at the conclusion of a job.
// Historically (prior to v3) the bootstrap was a shell script, but was ported to
// Golang for portability and testability
type Bootstrap struct {
	// Config provides the bootstrap configuration
	Config

	// Phases to execute, defaults to all phases
	Phases []string

	// Shell is the shell environment for the bootstrap
	shell *shell.Shell

	// Plugins are checkout out in the PluginPhase
	plugins []*pluginCheckout

	// Whether the checkout dir was created as part of checkout
	createdCheckoutDir bool
}

// Start runs the bootstrap and returns the exit code
func (b *Bootstrap) Start() (exitCode int) {
	// Check if not nil to allow for tests to overwrite shell
	if b.shell == nil {
		var err error
		b.shell, err = shell.New()
		if err != nil {
			fmt.Printf("Error creating shell: %v", err)
			return 1
		}

		b.shell.PTY = b.Config.RunInPty
		b.shell.Debug = b.Config.Debug
	}

	// Tear down the environment (and fire pre-exit hook) before we exit
	defer func() {
		if err := b.tearDown(); err != nil {
			b.shell.Errorf("Error tearing down bootstrap: %v", err)

			// this gets passed back via the named return
			exitCode = shell.GetExitCode(err)
		}
	}()

	// Initialize the environment, a failure here will still call the tearDown
	if err := b.setUp(); err != nil {
		b.shell.Errorf("Error setting up bootstrap: %v", err)
		return shell.GetExitCode(err)
	}

	var includePhase = func(phase string) bool {
		if len(b.Phases) == 0 {
			return true
		}
		for _, include := range b.Phases {
			if include == phase {
				return true
			}
		}
		return false
	}

	//  Execute the bootstrap phases in order
	var phaseErr error

	if includePhase(`plugin`) {
		phaseErr = b.PluginPhase()
	}

	if phaseErr == nil && includePhase(`checkout`) {
		phaseErr = b.CheckoutPhase()
	} else {
		checkoutDir, exists := b.shell.Env.Get(`BUILDKITE_BUILD_CHECKOUT_PATH`)
		if exists {
			_ = b.shell.Chdir(checkoutDir)
		}
	}

	if phaseErr == nil && includePhase(`command`) {
		phaseErr = b.CommandPhase()

		// Only upload artifacts as part of the command phase
		if err := b.uploadArtifacts(); err != nil {
			b.shell.Errorf("%v", err)
			return shell.GetExitCode(err)
		}
	}

	// Phase errors are where something of ours broke that merits a big red error
	// this won't include command failures, as we view that as more in the user space
	if phaseErr != nil {
		b.shell.Errorf("%v", phaseErr)
		return shell.GetExitCode(phaseErr)
	}

	// Use the exit code from the command phase
	exitStatus, _ := b.shell.Env.Get(`BUILDKITE_COMMAND_EXIT_STATUS`)
	exitStatusCode, _ := strconv.Atoi(exitStatus)

	return exitStatusCode
}

// executeHook runs a hook script with the hookRunner
func (b *Bootstrap) executeHook(name string, hookPath string, extraEnviron *env.Environment) error {
	if !fileExists(hookPath) {
		if b.Debug {
			b.shell.Commentf("Skipping %s hook, no script at \"%s\"", name, hookPath)
		}
		return nil
	}

	b.shell.Headerf("Running %s hook", name)

	// We need a script to wrap the hook script so that we can snaffle the changed
	// environment variables
	script, err := newHookScriptWrapper(hookPath)
	if err != nil {
		b.shell.Errorf("Error creating hook script: %v", err)
		return err
	}
	defer script.Close()

	cleanHookPath := hookPath

	// Show a relative path if we can
	if strings.HasPrefix(hookPath, b.shell.Getwd()) {
		var err error
		if cleanHookPath, err = filepath.Rel(b.shell.Getwd(), hookPath); err != nil {
			cleanHookPath = hookPath
		}
	}

	// Show the hook runner in debug, but the thing being run otherwise 💅🏻
	if b.Debug {
		b.shell.Commentf("A hook runner was written to \"%s\" with the following:", script.Path())
		b.shell.Promptf("%s", process.FormatCommand(script.Path(), nil))
	} else {
		b.shell.Promptf("%s", process.FormatCommand(cleanHookPath, []string{}))
	}

	// Run the wrapper script
	if err := b.shell.RunScript(script.Path(), extraEnviron); err != nil {
		exitCode := shell.GetExitCode(err)
		b.shell.Env.Set("BUILDKITE_LAST_HOOK_EXIT_STATUS", fmt.Sprintf("%d", exitCode))

		// Give a simpler error if it's just a shell exit error
		if shell.IsExitError(err) {
			return &shell.ExitError{
				Code:    exitCode,
				Message: fmt.Sprintf("The %s hook exited with status %d", name, exitCode),
			}
		}
		return err
	}

	// Store the last hook exit code for subsequent steps
	b.shell.Env.Set("BUILDKITE_LAST_HOOK_EXIT_STATUS", "0")

	// Get changed environment
	changes, err := script.Changes()
	if err != nil {
		return errors.Wrapf(err, "Failed to get environment")
	}

	// Finally, apply changes to the current shell and config
	b.applyEnvironmentChanges(changes.Env, changes.Dir)
	return nil
}

func (b *Bootstrap) applyEnvironmentChanges(environ *env.Environment, dir string) {
	if dir != b.shell.Getwd() {
		_ = b.shell.Chdir(dir)
	}

	// Do we even have any environment variables to change?
	if environ != nil && environ.Length() > 0 {
		// First, let see any of the environment variables are supposed
		// to change the bootstrap configuration at run time.
		bootstrapConfigEnvChanges := b.Config.ReadFromEnvironment(environ)

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

// Returns the absolute path to the best matching hook file in a path, or os.ErrNotExist if none is found
func (b *Bootstrap) findHookFile(hookDir string, name string) (string, error) {
	if runtime.GOOS == "windows" {
		// check for windows types first
		if p, err := shell.LookPath(name, hookDir, ".BAT;.CMD"); err == nil {
			return p, nil
		}
	}
	// otherwise chech for th default shell script
	if p := filepath.Join(hookDir, name); fileExists(p) {
		return p, nil
	}
	return "", os.ErrNotExist
}

func (b *Bootstrap) hasGlobalHook(name string) bool {
	_, err := b.globalHookPath(name)
	return err == nil
}

// Returns the absolute path to a global hook, or os.ErrNotExist if none is found
func (b *Bootstrap) globalHookPath(name string) (string, error) {
	return b.findHookFile(b.HooksPath, name)
}

// Executes a global hook if one exists
func (b *Bootstrap) executeGlobalHook(name string) error {
	if !b.hasGlobalHook(name) {
		return nil
	}
	p, err := b.globalHookPath(name)
	if err != nil {
		return err
	}
	return b.executeHook("global "+name, p, nil)
}

// Returns the absolute path to a local hook, or os.ErrNotExist if none is found
func (b *Bootstrap) localHookPath(name string) (string, error) {
	return b.findHookFile(filepath.Join(b.shell.Getwd(), ".buildkite", "hooks"), name)
}

func (b *Bootstrap) hasLocalHook(name string) bool {
	_, err := b.localHookPath(name)
	return err == nil
}

// Executes a local hook
func (b *Bootstrap) executeLocalHook(name string) error {
	if !b.hasLocalHook(name) {
		return nil
	}

	localHookPath, err := b.localHookPath(name)
	if err != nil {
		return nil
	}

	// For high-security configs, we allow the disabling of local hooks.
	localHooksEnabled := b.Config.LocalHooksEnabled

	// Allow hooks to disable local hooks by setting BUILDKITE_NO_LOCAL_HOOKS=true
	noLocalHooks, _ := b.shell.Env.Get(`BUILDKITE_NO_LOCAL_HOOKS`)
	if noLocalHooks == "true" || noLocalHooks == "1" {
		localHooksEnabled = false
	}

	if !localHooksEnabled {
		return fmt.Errorf("Refusing to run %s, local hooks are disabled", localHookPath)
	}

	return b.executeHook("local "+name, localHookPath, nil)
}

// Returns whether or not a file exists on the filesystem. We consider any
// error returned by os.Stat to indicate that the file doesn't exist. We could
// be specific and use os.IsNotExist(err), but most other errors also indicate
// that the file isn't there (or isn't available) so we'll just catch them all.
func fileExists(filename string) bool {
	_, err := os.Stat(filename)
	return err == nil
}

func dirForAgentName(agentName string) string {
	badCharsPattern := regexp.MustCompile("[[:^alnum:]]")
	return badCharsPattern.ReplaceAllString(agentName, "-")
}

// Given a repository, it will add the host to the set of SSH known_hosts on the machine
func addRepositoryHostToSSHKnownHosts(sh *shell.Shell, repository string) {
	if fileExists(repository) {
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

// Makes sure a file is executable
func addExecutePermissionToFile(filename string) error {
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
		path, _ := b.shell.Env.Get("PATH")
		b.shell.Env.Set("PATH", fmt.Sprintf("%s%s%s", b.BinPath, string(os.PathListSeparator), path))
	}

	// Set a BUILDKITE_BUILD_CHECKOUT_PATH unless one exists already. We do this here
	// so that the environment will have a checkout path to work with
	if _, exists := b.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH"); !exists {
		if b.BuildPath == "" {
			return fmt.Errorf("Must set either a BUILDKITE_BUILD_PATH or a BUILDKITE_BUILD_CHECKOUT_PATH")
		}
		b.shell.Env.Set("BUILDKITE_BUILD_CHECKOUT_PATH",
			filepath.Join(b.BuildPath, dirForAgentName(b.AgentName), b.OrganizationSlug, b.PipelineSlug))
	}

	// The job runner sets BUILDKITE_IGNORED_ENV with any keys that were ignored
	// or overwritten. This shows a warning to the user so they don't get confused
	// when their environment changes don't seem to do anything
	if ignored, exists := b.shell.Env.Get("BUILDKITE_IGNORED_ENV"); exists {
		b.shell.Headerf("Detected protected environment variables")
		b.shell.Commentf("Your pipeline environment has protected environment variables set. " +
			"These can only be set via hooks, plugins or the agent configuration.")

		for _, env := range strings.Split(ignored, ",") {
			b.shell.Warningf("Ignored %s", env)
		}

		b.shell.Printf("^^^ +++")
	}

	if b.Debug {
		b.shell.Headerf("Buildkite environment variables")
		for _, e := range b.shell.Env.ToSlice() {
			if strings.HasPrefix(e, "BUILDKITE_AGENT_ACCESS_TOKEN=") {
				b.shell.Printf("BUILDKITE_AGENT_ACCESS_TOKEN=******************")
			} else if strings.HasPrefix(e, "BUILDKITE") || strings.HasPrefix(e, "CI") || strings.HasPrefix(e, "PATH") {
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

	if err := b.executePluginHook("pre-exit"); err != nil {
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
		if !b.Config.LocalHooksEnabled {
			return fmt.Errorf("Plugins have been disabled on this agent with `--no-local-hooks`")
		} else if !b.Config.CommandEval {
			return fmt.Errorf("Plugins have been disabled on this agent with `--no-command-eval`")
		} else {
			return fmt.Errorf("Plugins have been disabled on this agent with `--no-plugins`")
		}
	}

	plugins, err := plugin.CreateFromJSON(b.Plugins)
	if err != nil {
		return errors.Wrap(err, "Failed to parse plugin definition")
	}

	b.plugins = []*pluginCheckout{}

	for _, p := range plugins {
		checkout, err := b.checkoutPlugin(p)
		if err != nil {
			return errors.Wrapf(err, "Failed to checkout plugin %s", p.Name())
		}
		if b.Config.PluginValidation {
			if b.Debug {
				b.shell.Commentf("Parsing plugin definition for %s from %s", p.Name(), checkout.CheckoutDir)
			}
			// parse the plugin definition from the plugin checkout dir
			checkout.Definition, err = plugin.LoadDefinitionFromDir(checkout.CheckoutDir)
			if err == plugin.ErrDefinitionNotFound {
				b.shell.Warningf("Failed to find plugin definition for plugin %s", p.Name())
			} else if err != nil {
				return err
			}
		}
		b.plugins = append(b.plugins, checkout)
	}

	if b.Config.PluginValidation {
		for _, checkout := range b.plugins {
			// This is nil if the definition failed to parse or is missing
			if checkout.Definition == nil {
				continue
			}

			val := &plugin.Validator{}
			result := val.Validate(checkout.Definition, checkout.Plugin.Configuration)

			if !result.Valid() {
				b.shell.Headerf("Plugin validation failed for %q", checkout.Plugin.Name())
				json, _ := json.Marshal(checkout.Plugin.Configuration)
				b.shell.Commentf("Plugin configuration JSON is %s", json)
				return result
			} else {
				b.shell.Commentf("Valid plugin configuration for %q", checkout.Plugin.Name())
			}
		}
	}

	// Now we can run plugin environment hooks too
	return b.executePluginHook("environment")
}

// Executes a named hook on all plugins that have it
func (b *Bootstrap) executePluginHook(name string) error {
	for _, p := range b.plugins {
		hookPath, err := b.findHookFile(p.HooksDir, name)
		if err != nil {
			continue
		}

		env, _ := p.ConfigurationToEnvironment()
		if err := b.executeHook("plugin "+p.Label()+" "+name, hookPath, env); err != nil {
			return err
		}
	}
	return nil
}

// If any plugin has a hook by this name
func (b *Bootstrap) hasPluginHook(name string) bool {
	for _, p := range b.plugins {
		if _, err := b.findHookFile(p.HooksDir, name); err == nil {
			return true
		}
	}
	return false
}

// Checkout a given plugin to the plugins directory and return that directory
func (b *Bootstrap) checkoutPlugin(p *plugin.Plugin) (*pluginCheckout, error) {
	// Get the identifer for the plugin
	id, err := p.Identifier()
	if err != nil {
		return nil, err
	}

	// Ensure the plugin directory exists, otherwise we can't create the lock
	err = os.MkdirAll(b.PluginsPath, 0777)
	if err != nil {
		return nil, err
	}

	// Try and lock this particular plugin while we check it out (we create
	// the file outside of the plugin directory so git clone doesn't have
	// a cry about the directory not being empty)
	pluginCheckoutHook, err := b.shell.LockFile(filepath.Join(b.PluginsPath, id+".lock"), time.Minute*5)
	if err != nil {
		return nil, err
	}
	defer pluginCheckoutHook.Unlock()

	// Create a path to the plugin
	directory := filepath.Join(b.PluginsPath, id)
	pluginGitDirectory := filepath.Join(directory, ".git")
	checkout := &pluginCheckout{
		Plugin:      p,
		CheckoutDir: directory,
		HooksDir:    filepath.Join(directory, "hooks"),
	}

	// Has it already been checked out?
	if fileExists(pluginGitDirectory) {
		// It'd be nice to show the current commit of the plugin, so
		// let's figure that out.
		headCommit, err := gitRevParseInWorkingDirectory(b.shell, directory, "--short=7", "HEAD")
		if err != nil {
			b.shell.Commentf("Plugin %q already checked out (can't `git rev-parse HEAD` plugin git directory)", p.Label())
		} else {
			b.shell.Commentf("Plugin %q already checked out (%s)", p.Label(), strings.TrimSpace(headCommit))
		}

		return checkout, nil
	}

	// Make the directory
	err = os.MkdirAll(directory, 0777)
	if err != nil {
		return nil, err
	}

	// Once we've got the lock, we need to make sure another process didn't already
	// checkout the plugin
	if fileExists(pluginGitDirectory) {
		b.shell.Commentf("Plugin \"%s\" already checked out", p.Label())
		return checkout, nil
	}

	repo, err := p.Repository()
	if err != nil {
		return nil, err
	}

	b.shell.Commentf("Plugin \"%s\" will be checked out to \"%s\"", p.Location, directory)

	if b.Debug {
		b.shell.Commentf("Checking if \"%s\" is a local repository", repo)
	}

	// Switch to the plugin directory
	previousWd := b.shell.Getwd()
	if err = b.shell.Chdir(directory); err != nil {
		return nil, err
	}

	// Switch back to the previous working directory
	defer b.shell.Chdir(previousWd)

	b.shell.Commentf("Switching to the plugin directory")

	if b.SSHKeyscan {
		addRepositoryHostToSSHKnownHosts(b.shell, repo)
	}

	// Plugin clones shouldn't use custom GitCloneFlags
	if err = b.shell.Run("git", "clone", "-v", "--", repo, "."); err != nil {
		return nil, err
	}

	// Switch to the version if we need to
	if p.Version != "" {
		b.shell.Commentf("Checking out `%s`", p.Version)
		if err = b.shell.Run("git", "checkout", "-f", p.Version); err != nil {
			return nil, err
		}
	}

	return checkout, nil
}

func (b *Bootstrap) removeCheckoutDir() error {
	checkoutPath, _ := b.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH")

	b.shell.Commentf("Removing %s", checkoutPath)
	if err := os.RemoveAll(checkoutPath); err != nil {
		return fmt.Errorf("Failed to remove \"%s\" (%s)", checkoutPath, err)
	}
	return nil
}

func (b *Bootstrap) createCheckoutDir() error {
	checkoutPath, _ := b.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH")

	if !fileExists(checkoutPath) {
		b.shell.Commentf("Creating \"%s\"", checkoutPath)
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
func (b *Bootstrap) CheckoutPhase() error {
	if err := b.executeGlobalHook("pre-checkout"); err != nil {
		return err
	}

	if err := b.executePluginHook("pre-checkout"); err != nil {
		return err
	}

	// Remove the checkout directory if BUILDKITE_CLEAN_CHECKOUT is present
	if b.CleanCheckout {
		b.shell.Headerf("Cleaning pipeline checkout")
		if err := b.removeCheckoutDir(); err != nil {
			return err
		}
	}

	b.shell.Headerf("Preparing working directory")

	// Make sure the build directory exists
	if err := b.createCheckoutDir(); err != nil {
		return err
	}

	// There can only be one checkout hook, either plugin or global, in that order
	switch {
	case b.hasPluginHook("checkout"):
		if err := b.executePluginHook("checkout"); err != nil {
			return err
		}
	case b.hasGlobalHook("checkout"):
		if err := b.executeGlobalHook("checkout"); err != nil {
			return err
		}
	default:
		err := retry.Do(func(s *retry.Stats) error {
			err := b.defaultCheckoutPhase()
			if err != nil {
				b.shell.Warningf("Checkout failed! %s (%s)", err, s)

				// Checkout can fail because of corrupted files in the checkout
				// which can leave the agent in a state where it keeps failing
				// This removes the checkout dir, which means the next checkout
				// will be a lot slower (clone vs fetch), but hopefully will
				// allow the agent to self-heal
				_ = b.removeCheckoutDir()
			}
			return err
		}, &retry.Config{Maximum: 3, Interval: 2 * time.Second})
		if err != nil {
			return err
		}
	}

	// Store the current value of BUILDKITE_BUILD_CHECKOUT_PATH, so we can detect if
	// one of the post-checkout hooks changed it.
	previousCheckoutPath, _ := b.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH")

	// Run post-checkout hooks
	if err := b.executeGlobalHook("post-checkout"); err != nil {
		return err
	}

	if err := b.executeLocalHook("post-checkout"); err != nil {
		return err
	}

	if err := b.executePluginHook("post-checkout"); err != nil {
		return err
	}

	// Capture the new checkout path so we can see if it's changed.
	newCheckoutPath, _ := b.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH")

	// If the working directory has been changed by a hook, log and switch to it
	if previousCheckoutPath != "" && previousCheckoutPath != newCheckoutPath {
		b.shell.Headerf("A post-checkout hook has changed the working directory to \"%s\"", newCheckoutPath)

		if err := b.shell.Chdir(newCheckoutPath); err != nil {
			return err
		}
	}

	return nil
}

func hasGitSubmodules(sh *shell.Shell) bool {
	return fileExists(filepath.Join(sh.Getwd(), ".gitmodules"))
}

// defaultCheckoutPhase is called by the CheckoutPhase if no global or plugin checkout
// hook exists. It performs the default checkout on the Repository provided in the config
func (b *Bootstrap) defaultCheckoutPhase() error {
	// Make sure the build directory exists
	if err := b.createCheckoutDir(); err != nil {
		return err
	}

	if b.SSHKeyscan {
		addRepositoryHostToSSHKnownHosts(b.shell, b.Repository)
	}

	// Does the git directory exist?
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
	if hasGitSubmodules(b.shell) {
		if err := gitCleanSubmodules(b.shell, b.GitCleanFlags); err != nil {
			return err
		}
	}

	if err := gitClean(b.shell, b.GitCleanFlags); err != nil {
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
		b.shell.Commentf("Fetch and checkout pull request head from GitHub")
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

	var gitSubmodules bool
	if !b.GitSubmodules && hasGitSubmodules(b.shell) {
		b.shell.Warningf("This repository has submodules, but submodules are disabled at an agent level")
	} else if b.GitSubmodules && hasGitSubmodules(b.shell) {
		b.shell.Commentf("Git submodules detected")
		gitSubmodules = true
	}

	if gitSubmodules {
		// submodules might need their fingerprints verified too
		if b.SSHKeyscan {
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

	// Grab author and commit information and send
	// it back to Buildkite. But before we do,
	// we'll check to see if someone else has done
	// it first.
	b.shell.Commentf("Checking to see if Git data needs to be sent to Buildkite")
	if err := b.shell.Run("buildkite-agent", "meta-data", "exists", "buildkite:git:commit"); err != nil {
		b.shell.Commentf("Sending Git commit information back to Buildkite")

		gitCommitOutput, err := b.shell.RunAndCapture("git", "--no-pager", "show", "HEAD", "-s", "--format=fuller", "--no-color")
		if err != nil {
			return err
		}

		if err = b.shell.Run("buildkite-agent", "meta-data", "set", "buildkite:git:commit", gitCommitOutput); err != nil {
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

	if err := b.executePluginHook("pre-command"); err != nil {
		return err
	}

	var commandExitError error

	// There can only be one command hook, so we check them in order of plugin, local
	switch {
	case b.hasPluginHook("command"):
		commandExitError = b.executePluginHook("command")
	case b.hasLocalHook("command"):
		commandExitError = b.executeLocalHook("command")
	case b.hasGlobalHook("command"):
		commandExitError = b.executeGlobalHook("command")
	default:
		commandExitError = b.defaultCommandPhase()
	}

	// If the command returned an exit that wasn't a `exec.ExitError`
	// (which is returned when the command is actually run, but fails),
	// then we'll show it in the log.
	if shell.IsExitError(commandExitError) {
		b.shell.Errorf("The command exited with status %d", shell.GetExitCode(commandExitError))
	} else if commandExitError != nil {
		b.shell.Errorf(commandExitError.Error())
	}

	// Expand the command header if the command fails for any reason
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

	if err := b.executePluginHook("post-command"); err != nil {
		return err
	}

	return nil
}

// defaultCommandPhase is executed if there is no global or plugin command hook
func (b *Bootstrap) defaultCommandPhase() error {
	// Make sure we actually have a command to run
	if strings.TrimSpace(b.Command) == "" {
		return fmt.Errorf("No command has been provided")
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

	var cmdToExec string

	// The shell gets parsed based on the operating system
	shell, err := shellwords.Split(b.Shell)
	if err != nil {
		return fmt.Errorf("Failed to split shell (%q) into tokens: %v", b.Shell, err)
	}

	if len(shell) == 0 {
		return fmt.Errorf("No shell set for bootstrap")
	}

	// Windows CMD.EXE is horrible and can't handle newline delimited commands. We write
	// a batch script so that it works, but we don't like it
	if strings.ToUpper(filepath.Base(shell[0])) == `CMD.EXE` {
		batchScript, err := b.writeBatchScript(b.Command)
		if err != nil {
			return err
		}
		defer os.Remove(batchScript)

		b.shell.Headerf("Running batch script")
		if b.Debug {
			contents, err := ioutil.ReadFile(batchScript)
			if err != nil {
				return err
			}
			b.shell.Commentf("Wrote batch script %s\n%s", batchScript, contents)
		}

		cmdToExec = batchScript
	} else if commandIsScript {
		// Make script executable
		if err = addExecutePermissionToFile(pathToCommand); err != nil {
			b.shell.Warningf("Error marking script %q as executable: %v", pathToCommand, err)
			return err
		}

		// Make the path relative to the shell working dir
		scriptPath, err := filepath.Rel(b.shell.Getwd(), pathToCommand)
		if err != nil {
			return err
		}

		b.shell.Headerf("Running script")
		cmdToExec = fmt.Sprintf(".%c%s", os.PathSeparator, scriptPath)
	} else {
		b.shell.Headerf("Running commands")
		cmdToExec = b.Command
	}

	// Support deprecated BUILDKITE_DOCKER* env vars
	if hasDeprecatedDockerIntegration(b.shell) {
		if b.Debug {
			b.shell.Commentf("Detected deprecated docker environment variables")
		}
		return runDeprecatedDockerIntegration(b.shell, []string{cmdToExec})
	}

	var cmd []string
	cmd = append(cmd, shell...)
	cmd = append(cmd, cmdToExec)

	if b.Debug {
		b.shell.Promptf("%s", process.FormatCommand(cmd[0], cmd[1:]))
	} else {
		b.shell.Promptf("%s", cmdToExec)
	}

	return b.shell.RunWithoutPrompt(cmd[0], cmd[1:]...)
}

func (b *Bootstrap) writeBatchScript(cmd string) (string, error) {
	scriptFile, err := shell.TempFileWithExtension(
		`buildkite-script.bat`,
	)
	if err != nil {
		return "", err
	}
	defer scriptFile.Close()

	var scriptContents = "@echo off\n"

	for _, line := range strings.Split(cmd, "\n") {
		if line != "" {
			scriptContents += line + "\n" + "if %errorlevel% neq 0 exit /b %errorlevel%\n"
		}
	}

	_, err = io.WriteString(scriptFile, scriptContents)
	if err != nil {
		return "", err
	}

	return scriptFile.Name(), nil

}

func (b *Bootstrap) uploadArtifacts() error {
	if b.AutomaticArtifactUploadPaths == "" {
		return nil
	}

	// Run pre-artifact hooks
	if err := b.executeGlobalHook("pre-artifact"); err != nil {
		return err
	}

	if err := b.executeLocalHook("pre-artifact"); err != nil {
		return err
	}

	if err := b.executePluginHook("pre-artifact"); err != nil {
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

	if err := b.executePluginHook("post-artifact"); err != nil {
		return err
	}

	return nil
}

// Check for ignored env variables from the job runner. Some
// env (e.g BUILDKITE_BUILD_PATH) can only be set from config or by hooks.
// If these env are set at a pipeline level, we rewrite them to BUILDKITE_X_BUILD_PATH
// and warn on them here so that users know what is going on
func (b *Bootstrap) ignoredEnv() []string {
	var ignored []string
	for _, env := range os.Environ() {
		if strings.HasPrefix(env, `BUILDKITE_X_`) {
			ignored = append(ignored, fmt.Sprintf("BUILDKITE_%s",
				strings.TrimPrefix(env, `BUILDKITE_X_`)))
		}
	}
	return ignored
}

type pluginCheckout struct {
	*plugin.Plugin
	*plugin.Definition
	CheckoutDir string
	HooksDir    string
}
