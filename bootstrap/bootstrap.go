package bootstrap

import (
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/buildkite/agent/agent"
	"github.com/buildkite/agent/bootstrap/shell"
	"github.com/buildkite/agent/env"

	"github.com/flynn-archive/go-shlex"
)

type Bootstrap struct {
	// The command to run
	Command string

	// The ID of the job being run
	JobID string

	// If the bootstrap is in debug mode
	Debug bool

	// The repository that needs to be cloned
	Repository string

	// The commit being built
	Commit string

	// The branch of the commit
	Branch string

	// The tag of the job commit
	Tag string

	// Optional refspec to override git fetch
	RefSpec string

	// Plugin definition for the job
	Plugins string

	// Should git submodules be checked out
	GitSubmodules bool

	// If the commit was part of a pull request, this will container the PR number
	PullRequest string

	// The provider of the the pipeline
	PipelineProvider string

	// Slug of the current organization
	OrganizationSlug string

	// Slug of the current pipeline
	PipelineSlug string

	// Name of the agent running the bootstrap
	AgentName string

	// Should the bootstrap remove an existing checkout before running the job
	CleanCheckout bool

	// Flags to pass to "git clone" command
	GitCloneFlags string

	// Flags to pass to "git clean" command
	GitCleanFlags string

	// Whether or not to run the hooks/commands in a PTY
	RunInPty bool

	// Are aribtary commands allowed to be executed
	CommandEval bool

	// Path where the builds will be run
	BuildPath string

	// Path to the buildkite-agent binary
	BinPath string

	// Path to the global hooks
	HooksPath string

	// Path to the plugins directory
	PluginsPath string

	// Paths to automatically upload as artifacts when the build finishes
	AutomaticArtifactUploadPaths string

	// A custom destination to upload artifacts to (i.e. s3://...)
	ArtifactUploadDestination string

	// Whether or not to automatically authorize SSH key hosts
	SSHFingerprintVerification bool

	// Shell is the shell environment for the bootstrap
	shell *shell.Shell
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

func dirForAgentName(agentName string) string {
	badCharsPattern := regexp.MustCompile("[[:^alnum:]]")
	return badCharsPattern.ReplaceAllString(agentName, "-")
}

var hasSchemePattern = regexp.MustCompile("^[^:]+://")
var scpLikeUrlPattern = regexp.MustCompile("^([^@]+@)?([^:]+):/?(.+)$")

func newGittableURL(ref string) (*url.URL, error) {
	if !hasSchemePattern.MatchString(ref) && scpLikeUrlPattern.MatchString(ref) {
		matched := scpLikeUrlPattern.FindStringSubmatch(ref)
		user := matched[1]
		host := matched[2]
		path := matched[3]

		ref = fmt.Sprintf("ssh://%s%s/%s", user, host, path)
	}

	return url.Parse(ref)
}

// Given a repostory, it will add the host to the set of SSH known_hosts on the machine
func (b *Bootstrap) addRepositoryHostToSSHKnownHosts(repository string) {
	// Try and parse the repository URL
	url, err := newGittableURL(repository)
	if err != nil {
		b.shell.Warningf("Could not parse \"%s\" as a URL - skipping adding host to SSH known_hosts", repository)
		return
	}

	knownHosts, err := findKnownHosts(b.shell)
	if err != nil {
		b.shell.Warningf("Failed to find SSH known_hosts file: %v", err)
		return
	}
	defer knownHosts.Unlock()

	// Clean up the SSH host and remove any key identifiers. See:
	// git@github.com-custom-identifier:foo/bar.git
	// https://buildkite.com/docs/agent/ssh-keys#creating-multiple-ssh-keys
	var repoSSHKeySwitcherRegex = regexp.MustCompile(`-[a-z0-9\-]+$`)
	host := repoSSHKeySwitcherRegex.ReplaceAllString(url.Host, "")

	if err = knownHosts.Add(host); err != nil {
		b.shell.Warningf("Failed to add `%s` to known_hosts file `%s`: %v'", host, url, err)
	}
}

// Executes a hook and applies any environment changes. The tricky thing with
// hooks is that they can modify the ENV of a bootstrap. And it's impossible to
// grab the ENV of a child process before it finishes, so we've got an awesome
// ugly hack to get around this.  We essentially have a bash script that writes
// the ENV to a file, runs the hook, then writes the ENV back to another file.
// Once all that has finished, we compare the files, and apply what ever
// changes to our running env. Cool huh?
func (b *Bootstrap) executeHook(name string, hookPath string, exitOnError bool, environ *env.Environment) error {
	// Check if the hook exists
	if fileExists(hookPath) {
		// Create a temporary file that we'll put the hook runner code in
		tempHookRunnerFile, err := shell.TempFileWithExtension(normalizeScriptFileName("buildkite-agent-bootstrap-hook-runner"))
		if err != nil {
			return err
		}

		// Ensure the hook script is executable
		if err := addExecutePermissiontoFile(tempHookRunnerFile.Name()); err != nil {
			return err
		}

		// We'll pump the ENV before the hook into this temp file
		tempEnvBeforeFile, err := shell.TempFileWithExtension("buildkite-agent-bootstrap-hook-env-before")
		if err != nil {
			return err
		}
		tempEnvBeforeFile.Close()

		// We'll then pump the ENV _after_ the hook into this temp file
		tempEnvAfterFile, err := shell.TempFileWithExtension("buildkite-agent-bootstrap-hook-env-after")
		if err != nil {
			return err
		}
		tempEnvAfterFile.Close()

		absolutePathToHook, err := filepath.Abs(hookPath)
		if err != nil {
			return fmt.Errorf("Failed to find absolute path to \"%s\" (%s)", hookPath, err)
		}

		// Create the hook runner code
		var hookScript string
		if runtime.GOOS == "windows" {
			hookScript = "@echo off\n" +
				"SETLOCAL ENABLEDELAYEDEXPANSION\n" +
				"SET > \"" + tempEnvBeforeFile.Name() + "\"\n" +
				"CALL \"" + absolutePathToHook + "\"\n" +
				"SET BUILDKITE_LAST_HOOK_EXIT_STATUS=!ERRORLEVEL!\n" +
				"SET > \"" + tempEnvAfterFile.Name() + "\"\n" +
				"EXIT %BUILDKITE_LAST_HOOK_EXIT_STATUS%"
		} else {
			hookScript = "#!/bin/bash\n" +
				"export -p > \"" + tempEnvBeforeFile.Name() + "\"\n" +
				". \"" + absolutePathToHook + "\"\n" +
				"BUILDKITE_LAST_HOOK_EXIT_STATUS=$?\n" +
				"export -p > \"" + tempEnvAfterFile.Name() + "\"\n" +
				"exit $BUILDKITE_LAST_HOOK_EXIT_STATUS"
		}

		// Write the hook script to the runner then close the file so
		// we can run it
		tempHookRunnerFile.WriteString(hookScript)
		tempHookRunnerFile.Close()

		if b.Debug {
			b.shell.Headerf("Preparing %s hook", name)
			b.shell.Commentf("A hook runner was written to \"%s\" with the following:", tempHookRunnerFile.Name())
			b.shell.Printf("%s", hookScript)
		}

		// Print to the screen we're going to run the hook
		b.shell.Headerf("Running %s hook", name)
		b.shell.Commentf("Executing \"%s\"", hookPath)

		// Create a copy of the current env
		previousEnviron := b.shell.Env.Copy()

		// If we have a custom ENV we want to apply
		if environ != nil {
			b.shell.Env = b.shell.Env.Merge(environ)
		}

		// Run the hook
		hookErr := b.shell.RunScript(tempHookRunnerFile.Name())

		// Restore the previous env
		b.shell.Env = previousEnviron

		// Exit from the bootstrapper if the hook exited
		if exitOnError && hookErr != nil {
			// return fmt.Errorf("The %s hook exited with a status of %d", name, hookExitStatus)
			return err
		}

		// Save the hook exit status so other hooks can get access to it
		b.shell.Env.Set("BUILDKITE_LAST_HOOK_EXIT_STATUS", fmt.Sprintf("%d", shell.GetExitCode(hookErr)))

		var beforeEnv *env.Environment
		var afterEnv *env.Environment

		// Compare the ENV current env with the after shots, then
		// modify the running env map with the changes.
		beforeEnvContents, err := ioutil.ReadFile(tempEnvBeforeFile.Name())
		if err != nil {
			return fmt.Errorf("Failed to read \"%s\" (%s)", tempEnvBeforeFile.Name(), err)
		} else {
			beforeEnv = env.FromExport(string(beforeEnvContents))
		}

		afterEnvContents, err := ioutil.ReadFile(tempEnvAfterFile.Name())
		if err != nil {
			return fmt.Errorf("Failed to read \"%s\" (%s)", tempEnvAfterFile.Name(), err)
		} else {
			afterEnv = env.FromExport(string(afterEnvContents))
		}

		// Remove the BUILDKITE_LAST_HOOK_EXIT_STATUS from the after
		// env (since we don't care about it)
		afterEnv.Remove("BUILDKITE_LAST_HOOK_EXIT_STATUS")

		diff := afterEnv.Diff(beforeEnv)
		if diff.Length() > 0 {
			b.shell.Headerf("Applying environment changes")
			for envDiffKey := range diff.ToMap() {
				b.shell.Commentf("%s changed", envDiffKey)
			}
			b.shell.Env = b.shell.Env.Merge(diff)
		}

		// Apply any config changes that may have occured
		b.applyEnvironmentConfigChanges()

		return hookErr
	}

	if b.Debug {
		b.shell.Headerf("Running %s hook", name)
		b.shell.Commentf("Skipping, no hook script found at \"%s\"", hookPath)
	}

	return nil
}

// Returns the absolute path to a global hook
func (b *Bootstrap) globalHookPath(name string) string {
	return filepath.Join(b.HooksPath, normalizeScriptFileName(name))
}

// Executes a global hook
func (b *Bootstrap) executeGlobalHook(name string) error {
	return b.executeHook("global "+name, b.globalHookPath(name), true, nil)
}

// Returns the absolute path to a local hook
func (b *Bootstrap) localHookPath(name string) string {
	return filepath.Join(b.shell.Getwd(), ".buildkite", "hooks", normalizeScriptFileName(name))
}

// Executes a local hook
func (b *Bootstrap) executeLocalHook(name string) error {
	return b.executeHook("local "+name, b.localHookPath(name), true, nil)
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

// Executes a plugin hook gracefully
func (b *Bootstrap) executePluginHookGracefully(plugins []*agent.Plugin, name string) error {
	for _, p := range plugins {
		env, _ := p.ConfigurationToEnvironment()
		path, err := b.pluginHookPath(p, name)
		if err != nil {
			return err
		}
		if err = b.executeHook("plugin "+p.Label()+" "+name, path, false, env); err != nil {
			return err
		}
	}

	return nil
}

// Executes a plugin hook
func (b *Bootstrap) executePluginHook(plugins []*agent.Plugin, name string) error {
	for _, p := range plugins {
		env, _ := p.ConfigurationToEnvironment()
		path, err := b.pluginHookPath(p, name)
		if err != nil {
			return err
		}
		if err = b.executeHook("plugin "+p.Label()+" "+name, path, true, env); err != nil {
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
			b.shell.Warningf("Failed to find path to %v", path)
			return false
		}
		if fileExists(path) {
			return true
		}
	}

	return false
}

// Checks to see if the bootstrap configuration has changed at runtime, and
// applies them if they've changed
func (b *Bootstrap) applyEnvironmentConfigChanges() {
	artifactPathsChanged := false
	artifactUploadDestinationChanged := false
	gitCloneFlagsChanged := false
	gitCleanFlagsChanged := false
	refSpecChanged := false

	if b.shell.Env.Exists("BUILDKITE_ARTIFACT_PATHS") {
		envArifactPaths := b.shell.Env.Get("BUILDKITE_ARTIFACT_PATHS")

		if envArifactPaths != b.AutomaticArtifactUploadPaths {
			b.AutomaticArtifactUploadPaths = envArifactPaths
			artifactPathsChanged = true
		}
	}

	if b.shell.Env.Exists("BUILDKITE_ARTIFACT_UPLOAD_DESTINATION") {
		envUploadDestination := b.shell.Env.Get("BUILDKITE_ARTIFACT_UPLOAD_DESTINATION")

		if envUploadDestination != b.ArtifactUploadDestination {
			b.ArtifactUploadDestination = envUploadDestination
			artifactUploadDestinationChanged = true
		}
	}

	if b.shell.Env.Exists("BUILDKITE_GIT_CLONE_FLAGS") {
		envGitCloneFlags := b.shell.Env.Get("BUILDKITE_GIT_CLONE_FLAGS")

		if envGitCloneFlags != b.GitCloneFlags {
			b.GitCloneFlags = envGitCloneFlags
			gitCloneFlagsChanged = true
		}
	}

	if b.shell.Env.Exists("BUILDKITE_GIT_CLEAN_FLAGS") {
		envGitCleanFlags := b.shell.Env.Get("BUILDKITE_GIT_CLEAN_FLAGS")

		if envGitCleanFlags != b.GitCleanFlags {
			b.GitCleanFlags = envGitCleanFlags
			gitCleanFlagsChanged = true
		}
	}

	if b.shell.Env.Exists("BUILDKITE_REFSPEC") {
		refSpec := b.shell.Env.Get("BUILDKITE_REFSPEC")

		if refSpec != b.RefSpec {
			b.RefSpec = refSpec
			refSpecChanged = true
		}
	}

	if artifactPathsChanged || artifactUploadDestinationChanged || gitCleanFlagsChanged || gitCloneFlagsChanged || refSpecChanged {
		b.shell.Headerf("Bootstrap configuration has changed")

		if artifactPathsChanged {
			b.shell.Commentf("BUILDKITE_ARTIFACT_PATHS is now \"%s\"", b.AutomaticArtifactUploadPaths)
		}

		if artifactUploadDestinationChanged {
			b.shell.Commentf("BUILDKITE_ARTIFACT_UPLOAD_DESTINATION is now \"%s\"", b.ArtifactUploadDestination)
		}

		if gitCleanFlagsChanged {
			b.shell.Commentf("BUILDKITE_GIT_CLEAN_FLAGS is now \"%s\"", b.GitCleanFlags)
		}

		if gitCloneFlagsChanged {
			b.shell.Commentf("BUILDKITE_GIT_CLONE_FLAGS is now \"%s\"", b.GitCloneFlags)
		}

		if refSpecChanged {
			b.shell.Commentf("BUILDKITE_REFSPEC is now \"%s\"", b.RefSpec)
		}
	}
}

func (b *Bootstrap) gitClean() error {
	gitCleanFlags, err := shlex.Split(b.GitCleanFlags)
	if err != nil {
		return fmt.Errorf("There was an error trying to split `%s` into arguments (%s)", b.GitCleanFlags, err)
	}

	// Clean up the repository
	gitCleanRepoArguments := []string{"clean"}
	gitCleanRepoArguments = append(gitCleanRepoArguments, gitCleanFlags...)
	if err = b.shell.RunCommand("git", gitCleanRepoArguments...); err != nil {
		return err
	}

	// Also clean up submodules if we can
	if b.GitSubmodules {
		gitCleanSubmoduleArguments := []string{"submodule", "foreach", "--recursive", "git", "clean"}
		gitCleanSubmoduleArguments = append(gitCleanSubmoduleArguments, gitCleanFlags...)

		if err = b.shell.RunCommand("git", gitCleanSubmoduleArguments...); err != nil {
			return err
		}
	}

	return nil
}

func (b *Bootstrap) gitEnumerateSubmoduleURLs() ([]string, error) {
	urls := []string{}

	// The output of this command looks like:
	// Entering 'vendor/docs'
	// git@github.com:buildkite/docs.git
	// Entering 'vendor/frontend'
	// git@github.com:buildkite/frontend.git
	// Entering 'vendor/frontend/vendor/emojis'
	// git@github.com:buildkite/emojis.git
	gitSubmoduleOutput, err := b.shell.RunCommandSilentlyAndCaptureOutput(
		"git", "submodule", "foreach", "--recursive", "git", "remote", "get-url", "origin")
	if err != nil {
		return nil, err
	}

	// splits into "Entering" "'vendor/blah'" "git@github.com:blah/.."
	// this should work for windows and unix line endings
	for idx, val := range strings.Fields(gitSubmoduleOutput) {
		// every third element to get the git@github.com:blah bit
		if idx%3 == 2 {
			urls = append(urls, val)
		}
	}

	return urls, nil
}

type BootstrapPhaseContext struct {
	Plugins []*agent.Plugin
}

func (b *Bootstrap) Start() error {
	var err error

	b.shell, err = shell.New()
	if err != nil {
		return err
	}

	b.shell.AddExitHandler(func(err error) {
		if b.Debug {
			b.shell.Commentf("Firing exit handler with %v", err)
		}
		os.Exit(shell.GetExitCode(err))
	})

	var ctx BootstrapPhaseContext

	var phases = []func(ctx BootstrapPhaseContext) error{
		b.SetupPhase,
		b.EnvironmentPhase,
		b.PluginPhase,
		b.CheckoutPhase,
		b.CommandPhase,
		b.PreExitPhase,
	}

	for _, phase := range phases {
		if err = phase(ctx); err != nil {
			b.shell.Exit(err)
			return err
		}
	}

	return nil
}

// SetupPhase prepares the Buildkite environment environment
func (b *Bootstrap) SetupPhase(ctx BootstrapPhaseContext) error {
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

	return nil
}

// EnvironmentPhase is a place for people to set up environment variables that
// might be needed for their build scripts, such as secret tokens and other information.
func (b *Bootstrap) EnvironmentPhase(ctx BootstrapPhaseContext) error {
	// The global environment hook
	//
	// It's important to do this before checking out plugins, in case you want
	// to use the global environment hook to whitelist the plugins that are
	// allowed to be used.
	return b.executeGlobalHook("environment")
}

// PluginPhase is where plugins that weren't filtered in the Environment phase are
// checked out and made available to later phases
func (b *Bootstrap) PluginPhase(ctx BootstrapPhaseContext) error {
	if b.Plugins != "" {
		b.shell.Headerf("Setting up plugins")

		// Make sure we have a plugin path before trying to do anything
		if b.PluginsPath == "" {
			return fmt.Errorf("Can't checkout plugins without a `plugins-path`")
		}

		var err error
		ctx.Plugins, err = agent.CreatePluginsFromJSON(b.Plugins)
		if err != nil {
			return fmt.Errorf("Failed to parse plugin definition (%s)", err)
		}

		for _, p := range ctx.Plugins {
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

				// Try and lock this paticular plugin while we
				// check it out (we create the file outside of
				// the plugin directory so git clone doesn't
				// have a cry about the directory not being empty)
				pluginCheckoutHook, err := b.shell.LockFileWithTimeout(filepath.Join(b.PluginsPath, id+".lock"), time.Minute*5)
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
					b.addRepositoryHostToSSHKnownHosts(repo)
				}

				// Plugin clones shouldn't use custom GitCloneFlags
				if err = b.shell.RunCommand("git", "clone", "-v", "--", repo, "."); err != nil {
					return err
				}

				// Switch to the version if we need to
				if p.Version != "" {
					b.shell.Commentf("Checking out \"%s\"", p.Version)
					if err = b.shell.RunCommand("git", "checkout", "-f", p.Version); err != nil {
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
	}

	// Now we can run plugin environment hooks too
	return b.executePluginHook(ctx.Plugins, "environment")
}

// CheckoutPhase creates the build directory and makes sure we're running the
// build at the right commit.
func (b *Bootstrap) CheckoutPhase(ctx BootstrapPhaseContext) error {
	if err := b.executeGlobalHook("pre-checkout"); err != nil {
		return err
	}

	if err := b.executePluginHook(ctx.Plugins, "pre-checkout"); err != nil {
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

	// Run a custom `checkout` hook if it's present
	if fileExists(b.globalHookPath("checkout")) {
		if err := b.executeGlobalHook("checkout"); err != nil {
			return err
		}
	} else if b.pluginHookExists(ctx.Plugins, "checkout") {
		if err := b.executePluginHook(ctx.Plugins, "checkout"); err != nil {
			return err
		}
	} else {
		if b.SSHFingerprintVerification {
			b.addRepositoryHostToSSHKnownHosts(b.Repository)
		}

		// Do we need to do a git checkout?
		existingGitDir := filepath.Join(b.shell.Getwd(), ".git")
		if fileExists(existingGitDir) {
			// Update the the origin of the repository so we can
			// gracefully handle repository renames
			if err := b.shell.RunCommand("git", "remote", "set-url", "origin", b.Repository); err != nil {
				return err
			}
		} else {
			gitCloneFlags, err := shlex.Split(b.GitCloneFlags)
			if err != nil {
				return fmt.Errorf("There was an error trying to split `%s` into arguments (%s)", b.GitCloneFlags, err)
			}

			gitCloneArguments := []string{"clone"}
			gitCloneArguments = append(gitCloneArguments, gitCloneFlags...)
			gitCloneArguments = append(gitCloneArguments, "--", b.Repository, ".")

			if err = b.shell.RunCommand("git", gitCloneArguments...); err != nil {
				return err
			}
		}

		// Git clean prior to checkout
		if err := b.gitClean(); err != nil {
			return err
		}

		// If a refspec is provided then use it instead.
		// i.e. `refs/not/a/head`
		if b.RefSpec != "" {
			// Convert RefSpec's like this:
			//     "+refs/heads/*:refs/remotes/origin/* +refs/tags/*:refs/tags/*"
			// Into...
			//     "+refs/heads/*:refs/remotes/origin/*" "+refs/tags/*:refs/tags/*"
			// Into multiple arguments for `git fetch`
			refSpecTargets, err := shlex.Split(b.RefSpec)
			if err != nil {
				return fmt.Errorf("There was an error trying to split `%s` into arguments (%s)", b.RefSpec, err)
			}

			b.shell.Commentf("Fetch and checkout custom refspec")

			refSpecArguments := append([]string{"fetch", "-v", "--prune", "origin"}, refSpecTargets...)
			if err = b.shell.RunCommand("git", refSpecArguments...); err != nil {
				return err
			}
			if err = b.shell.RunCommand("git", "checkout", "-f", b.Commit); err != nil {
				return err
			}

			// GitHub has a special ref which lets us fetch a pull request head, whether
			// or not there is a current head in this repository or another which
			// references the commit. We presume a commit sha is provided. See:
			// https://help.github.com/articles/checking-out-pull-requests-locally/#modifying-an-inactive-pull-request-locally
		} else if b.PullRequest != "false" && strings.Contains(b.PipelineProvider, "github") {
			b.shell.Commentf("Fetch and checkout pull request head")

			if err := b.shell.RunCommand("git", "fetch", "-v", "origin", "refs/pull/"+b.PullRequest+"/head"); err != nil {
				return err
			}

			gitFetchHead, _ := b.shell.RunCommandSilentlyAndCaptureOutput("git", "rev-parse", "FETCH_HEAD")
			b.shell.Commentf("FETCH_HEAD is now %s", strings.TrimSpace(gitFetchHead))

			if err := b.shell.RunCommand("git", "checkout", "-f", b.Commit); err != nil {
				return err
			}

			// If the commit is "HEAD" then we can't do a commit-specific fetch and will
			// need to fetch the remote head and checkout the fetched head explicitly.
		} else if b.Commit == "HEAD" {
			b.shell.Commentf("Fetch and checkout remote branch HEAD commit")
			if err := b.shell.RunCommand("git", "fetch", "-v", "--prune", "origin", b.Branch); err != nil {
				return err
			}
			if err := b.shell.RunCommand("git", "checkout", "-f", "FETCH_HEAD"); err != nil {
				return err
			}

			// Otherwise fetch and checkout the commit directly. Some repositories don't
			// support fetching a specific commit so we fall back to fetching all heads
			// and tags, hoping that the commit is included.
		} else {
			b.shell.Commentf("Fetch and checkout commit")
			if err := b.shell.RunCommand("git", "fetch", "-v", "origin", b.Commit); err != nil {
				// By default `git fetch origin` will only fetch tags which are
				// reachable from a fetches branch. git 1.9.0+ changed `--tags` to
				// fetch all tags in addition to the default refspec, but pre 1.9.0 it
				// excludes the default refspec.
				gitFetchRefspec, _ := b.shell.RunCommandSilentlyAndCaptureOutput("git", "config", "remote.origin.fetch")
				if err := b.shell.RunCommand("git", "fetch", "-v", "--prune", "origin", gitFetchRefspec, "+refs/tags/*:refs/tags/*"); err != nil {
					return err
				}
			}
			if err := b.shell.RunCommand("git", "checkout", "-f", b.Commit); err != nil {
				return err
			}
		}

		if b.GitSubmodules {
			// submodules might need their fingerprints verified too
			if b.SSHFingerprintVerification {
				b.shell.Commentf("Checking to see if submodule urls need to be added to known_hosts")
				submoduleRepos, err := b.gitEnumerateSubmoduleURLs()
				if err != nil {
					b.shell.Warningf("Failed to enumerate git submodules: %v", err)
				} else {
					for _, repository := range submoduleRepos {
						b.addRepositoryHostToSSHKnownHosts(repository)
					}
				}
			}

			// `submodule sync` will ensure the .git/config
			// matches the .gitmodules file.  The command
			// is only available in git version 1.8.1, so
			// if the call fails, continue the bootstrap
			// script, and show an informative error.
			if err := b.shell.RunCommand("git", "submodule", "sync", "--recursive"); err != nil {
				gitVersionOutput, _ := b.shell.RunCommandSilentlyAndCaptureOutput("git", "--version")
				b.shell.Warningf("Failed to recursively sync git submodules. This is most likely because you have an older version of git installed (" + gitVersionOutput + ") and you need version 1.8.1 and above. If you're using submodules, it's highly recommended you upgrade if you can.")
			}

			if err := b.shell.RunCommand("git", "submodule", "update", "--init", "--recursive", "--force"); err != nil {
				return err
			}
			if err := b.shell.RunCommand("git", "submodule", "foreach", "--recursive", "git", "reset", "--hard"); err != nil {
				return err
			}
		}

		// Git clean after checkout
		if err := b.gitClean(); err != nil {
			return err
		}

		if b.shell.Env.Get("BUILDKITE_AGENT_ACCESS_TOKEN") == "" {
			b.shell.Warningf("Skipping sending Git information to Buildkite as $BUILDKITE_AGENT_ACCESS_TOKEN is missing")
		} else {
			// Grab author and commit information and send
			// it back to Buildkite. But before we do,
			// we'll check to see if someone else has done
			// it first.
			b.shell.Commentf("Checking to see if Git data needs to be sent to Buildkite")
			if err := b.shell.RunCommand("buildkite-agent", "meta-data", "exists", "buildkite:git:commit"); err != nil {
				b.shell.Commentf("Sending Git commit information back to Buildkite")

				gitCommitOutput, _ := b.shell.RunCommandSilentlyAndCaptureOutput("git", "show", "HEAD", "-s", "--format=fuller", "--no-color")
				gitBranchOutput, _ := b.shell.RunCommandSilentlyAndCaptureOutput("git", "branch", "--contains", "HEAD", "--no-color")

				if err = b.shell.RunCommand("buildkite-agent", "meta-data", "set", "buildkite:git:commit", gitCommitOutput); err != nil {
					return err
				}
				if err = b.shell.RunCommand("buildkite-agent", "meta-data", "set", "buildkite:git:branch", gitBranchOutput); err != nil {
					return err
				}
			}
		}
	}

	// Store the current value of BUILDKITE_BUILD_CHECKOUT_PATH, so we can detect if
	// one of the post-checkout hooks changed it.
	previousCheckoutPath := b.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH")

	// Run the `post-checkout` global hook
	if err := b.executeGlobalHook("post-checkout"); err != nil {
		return err
	}

	// Run the `post-checkout` local hook
	if err := b.executeLocalHook("post-checkout"); err != nil {
		return err
	}

	// Run the `post-checkout` plugin hook
	if err := b.executePluginHook(ctx.Plugins, "post-checkout"); err != nil {
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

// CommandPhase determines how to run the build, and then runs it
func (b *Bootstrap) CommandPhase(ctx BootstrapPhaseContext) error {
	if err := b.executeGlobalHook("pre-command"); err != nil {
		return err
	}

	if err := b.executeLocalHook("pre-command"); err != nil {
		return err
	}

	if err := b.executePluginHook(ctx.Plugins, "pre-command"); err != nil {
		return err
	}

	var commandExitError error

	// Run either a custom `command` hook, or the default command runner.
	// We need to manually run these hooks so we can customize their
	// `exitOnError` behaviour
	localCommandHookPath := b.localHookPath("command")
	globalCommandHookPath := b.globalHookPath("command")

	if fileExists(localCommandHookPath) {
		commandExitError = b.executeHook("local command", localCommandHookPath, false, nil)
	} else if fileExists(globalCommandHookPath) {
		commandExitError = b.executeHook("global command", globalCommandHookPath, false, nil)
	} else if b.pluginHookExists(ctx.Plugins, "command") {
		commandExitError = b.executePluginHookGracefully(ctx.Plugins, "command")
	} else {
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
				return fmt.Errorf("Failed to write to \"%s\" (%s)", buildScriptPath, err)
			}
		}

		// Ensure it can be executed
		if err = addExecutePermissiontoFile(buildScriptPath); err != nil {
			return err
		}

		// Show we're running the script
		b.shell.Headerf("%s", headerLabel)
		if promptDisplay != "" {
			b.shell.Promptf("%s", promptDisplay)
		}

		commandExitError = b.shell.RunScript(buildScriptPath)
	}

	// Expand the command header if it fails
	if commandExitError != nil {
		b.shell.Printf("^^^ +++")
	}

	// Save the command exit status to the env so hooks + plugins can access it
	b.shell.Env.Set("BUILDKITE_COMMAND_EXIT_STATUS", fmt.Sprintf("%d", shell.GetExitCode(commandExitError)))

	// Run the `post-command` global hook
	if err := b.executeGlobalHook("post-command"); err != nil {
		return err
	}

	// Run the `post-command` local hook
	if err := b.executeLocalHook("post-command"); err != nil {
		return err
	}

	// Run the `post-command` plugin hook
	if err := b.executePluginHook(ctx.Plugins, "post-command"); err != nil {
		return err
	}

	return commandExitError
}

func (b *Bootstrap) ArtifactPhase(ctx BootstrapPhaseContext) error {
	if b.AutomaticArtifactUploadPaths != "" {
		// Run the `pre-artifact` global hook
		if err := b.executeGlobalHook("pre-artifact"); err != nil {
			return err
		}

		// Run the `pre-artifact` local hook
		if err := b.executeLocalHook("pre-artifact"); err != nil {
			return err
		}

		// Run the `pre-artifact` plugin hook
		if err := b.executePluginHook(ctx.Plugins, "pre-artifact"); err != nil {
			return err
		}

		// Run the artifact upload command
		b.shell.Headerf("Uploading artifacts")
		if err := b.shell.RunCommand("buildkite-agent", "artifact", "upload", b.AutomaticArtifactUploadPaths, b.ArtifactUploadDestination); err != nil {
			return err
		}

		// Run the `post-artifact` global hook
		if err := b.executeGlobalHook("post-artifact"); err != nil {
			return err
		}

		// Run the `post-artifact` local hook
		if err := b.executeLocalHook("post-artifact"); err != nil {
			return err
		}

		// Run the `post-artifact` plugin hook
		if err := b.executePluginHook(ctx.Plugins, "post-artifact"); err != nil {
			return err
		}
	}

	return nil
}

func (b *Bootstrap) PreExitPhase(ctx BootstrapPhaseContext) error {
	// Run the `pre-exit` global hook
	if err := b.executeGlobalHook("pre-exit"); err != nil {
		return err
	}

	// Run the `pre-exit` local hook
	if err := b.executeLocalHook("pre-exit"); err != nil {
		return err
	}

	// Run the `pre-exit` plugin hook
	if err := b.executePluginHook(ctx.Plugins, "pre-exit"); err != nil {
		return err
	}

	return nil
}
