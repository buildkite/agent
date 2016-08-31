package agent

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/buildkite/agent/shell"
	"github.com/buildkite/agent/shell/windows"
	"github.com/flynn-archive/go-shlex"
	"github.com/mitchellh/go-homedir"
	"github.com/nightlyone/lockfile"
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

	// The running environment for the bootstrap file as each task runs
	env *shell.Environment

	// Current working directory that shell commands get executed in
	wd string
}

// Prints a line of output
func printf(format string, v ...interface{}) {
	fmt.Printf("%s\n", fmt.Sprintf(format, v...))
}

// Prints a bootstrap formatted header
func headerf(format string, v ...interface{}) {
	fmt.Printf("~~~ %s\n", fmt.Sprintf(format, v...))
}

// Prints an info statement
func commentf(format string, v ...interface{}) {
	fmt.Printf("\033[90m# %s\033[0m\n", fmt.Sprintf(format, v...))
}

// Shows a buildkite boostrap error
func errorf(format string, v ...interface{}) {
	printf("\033[31m🚨 Buildkite Error: %s\033[0m", fmt.Sprintf(format, v...))
	printf("^^^ +++")
}

// Shows a buildkite boostrap warning
func warningf(format string, v ...interface{}) {
	printf("\033[33m⚠️ Buildkite Warning: %s\033[0m", fmt.Sprintf(format, v...))
	printf("^^^ +++")
}

// Shows the error text and exits the bootstrap
func exitf(format string, v ...interface{}) {
	errorf(format, v...)
	os.Exit(1)
}

// Prints a shell prompt
func promptf(format string, v ...interface{}) {
	if runtime.GOOS == "windows" {
		fmt.Printf("\033[90m>\033[0m %s\n", fmt.Sprintf(format, v...))
	} else {
		fmt.Printf("\033[90m$\033[0m %s\n", fmt.Sprintf(format, v...))
	}
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
	} else {
		return filename
	}
}

// Changes the permission of a file so it can be executed
func addExecutePermissiontoFile(filename string) {
	s, err := os.Stat(filename)
	if err != nil {
		exitf("Failed to retrieve file information of \"%s\" (%s)", filename, err)
	}

	err = os.Chmod(filename, s.Mode()|0100)
	if err != nil {
		exitf("Failed to mark \"%s\" as executable (%s)", filename, err)
	}
}

func dirForAgentName(agentName string) string {
	badCharsPattern := regexp.MustCompile("[[:^alnum:]]")
	return badCharsPattern.ReplaceAllString(agentName, "-")
}

var tempFileNumber int

// Creates a temporary file. Implementation has been copied from
func createTempFile(filename string) *os.File {
	extension := filepath.Ext(filename)
	basename := strings.TrimSuffix(filename, extension)

	// Create the file
	tempFile, err := ioutil.TempFile("", basename+"-")
	if err != nil {
		exitf("Failed to create temporary file \"%s\" (%s)", filename, err)
	}

	// Do we need to rename the file?
	if extension != "" {
		// Close the currently open tempfile
		tempFile.Close()

		// Rename it
		newTempFileName := tempFile.Name() + extension
		err = os.Rename(tempFile.Name(), newTempFileName)
		if err != nil {
			exitf("Failed to rename \"%s\" to \"%s\" (%s)", tempFile.Name(), newTempFileName, err)
		}

		// Open it again
		tempFile, err = os.OpenFile(newTempFileName, os.O_RDWR|os.O_EXCL, 0600)
		if err != nil {
			exitf("Failed to open temporary file \"%s\" (%s)", newTempFileName, err)
		}
	}

	return tempFile
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

// If a error exists, it will exit the bootstrap with an error
func checkShellError(err error, cmd *shell.Command) {
	if err != nil {
		exitf("There was an error running `%s` (%s)", cmd, err)
	}
}

// Aquires a lock on the directory
func acquireLock(path string, seconds int) (*lockfile.Lockfile, error) {
	absolutePathToLock, err := filepath.Abs(path)
	if err != nil {
		exitf("Failed to find absolute path to lock \"%s\" (%s)", path, err)
	}

	lock, err := lockfile.New(absolutePathToLock)
	if err != nil {
		return nil, fmt.Errorf("Failed to create lock \"%s\" (%s)", absolutePathToLock, err)
	}

	// Aquire the lock. Keep trying the lock until we get it
	attempts := 0
	for true {
		err := lock.TryLock()
		if err != nil {
			if err == lockfile.ErrBusy {
				// Keey track of how many times we tried to get the
				// lock
				attempts += 1
				if attempts > seconds {
					return nil, fmt.Errorf("Gave up trying to aquire a lock on \"%s\" (%s)", absolutePathToLock, err)
				}

				// Try again in a second
				commentf("Could not aquire lock on \"%s\" (%s)", absolutePathToLock, err)
				commentf("Trying again in 1 second...")
				time.Sleep(1 * time.Second)
			} else {
				return nil, err
			}
		} else {
			break
		}
	}

	return &lock, nil
}

// Returns the current working directory. Returns the current processes working
// directory if one has not been set directly.
func (b *Bootstrap) currentWorkingDirectory() string {
	if b.wd == "" {
		wd, err := os.Getwd()
		if err != nil {
			exitf("Failed to find current working directory (%s)", err)
		}

		return wd
	}

	return b.wd
}

// Changes the working directory of the bootstrap file
func (b *Bootstrap) changeWorkingDirectory(path string) {
	commentf("Changing working directory to \"%s\"", path)

	// If the path isn't absolute, prefix it with the current working
	// directory.
	if !filepath.IsAbs(path) {
		path = filepath.Join(b.currentWorkingDirectory(), path)
	}

	if fileExists(path) {
		b.wd = path
	} else {
		exitf("Failed to change working: directory does not exist")
	}
}

// Creates a shell command ready for running
func (b *Bootstrap) newCommand(command string, args ...string) *shell.Command {
	return &shell.Command{Command: command, Args: args, Env: b.env, Dir: b.currentWorkingDirectory()}
}

// Run a command without showing a prompt or the output to the user
func (b *Bootstrap) runCommandSilentlyAndCaptureOutput(command string, args ...string) (string, error) {
	cmd := b.newCommand(command, args...)

	var buffer bytes.Buffer
	_, err := shell.Run(cmd, &shell.Config{Writer: &buffer})

	return strings.TrimSpace(buffer.String()), err
}

// Run a command and return it's exit status
func (b *Bootstrap) runCommandGracefully(command string, args ...string) int {
	cmd := b.newCommand(command, args...)

	promptf("%s", cmd)

	process, err := shell.Run(cmd, &shell.Config{Writer: os.Stdout})
	checkShellError(err, cmd)

	return process.ExitStatus()
}

// Runs a script on the file system
func (b *Bootstrap) runScript(command string) int {
	var cmd *shell.Command
	if runtime.GOOS == "windows" {
		cmd = b.newCommand(command)
	} else {
		// If you run a script on Linux that doesn't have the
		// #!/bin/bash thingy at the top, it will fail to run with a
		// "exec format error" error. You can solve it by adding the
		// #!/bin/bash line to the top of the file, but that's
		// annoying, and people generally forget it, so we'll make it
		// easy on them and add it for them here.
		//
		// We also need to make sure the script we pass has quotes
		// around it, otherwise `/bin/bash -c run script with space.sh`
		// fails.
		cmd = b.newCommand("/bin/bash", "-c", `"`+strings.Replace(command, `"`, `\"`, -1)+`"`)
	}

	process, err := shell.Run(cmd, &shell.Config{Writer: os.Stdout, PTY: b.RunInPty})
	checkShellError(err, cmd)

	return process.ExitStatus()
}

// Run a command, and if it fails, exit the bootstrap
func (b *Bootstrap) runCommand(command string, args ...string) {
	exitStatus := b.runCommandGracefully(command, args...)

	if exitStatus != 0 {
		os.Exit(exitStatus)
	}
}

// Given a repostory, it will add the host to the set of SSH known_hosts on the
// machine
func (b *Bootstrap) addRepositoryHostToSSHKnownHosts(repository string) {
	// Try and parse the repository URL
	url, err := newGittableURL(repository)
	if err != nil {
		warningf("Could not parse \"%s\" as a URL - skipping adding host to SSH known_hosts", repository)
		return
	}

	userHomePath, err := homedir.Dir()
	if err != nil {
		warningf("Could not find the current users home directory (%s)", err)
		return
	}

	// Construct paths to the known_hosts file
	sshDirectory := filepath.Join(userHomePath, ".ssh")
	knownHostPath := filepath.Join(sshDirectory, "known_hosts")

	// Ensure a directory exists and known_host file exist
	if !fileExists(knownHostPath) {
		os.MkdirAll(sshDirectory, 0755)
		ioutil.WriteFile(knownHostPath, []byte(""), 0644)
	}

	// Create a lock on the known_host file so other agents don't try and
	// change it at the same time
	knownHostLock, err := acquireLock(filepath.Join(sshDirectory, "known_hosts.lock"), 30)
	if err != nil {
		exitf("%s", err)
	}
	defer knownHostLock.Unlock()

	// Clean up the SSH host and remove any key identifiers. See:
	// git@github.com-custom-identifier:foo/bar.git
	// https://buildkite.com/docs/agent/ssh-keys#creating-multiple-ssh-keys
	var repoSSHKeySwitcherRegex = regexp.MustCompile(`-[a-z0-9\-]+$`)
	host := repoSSHKeySwitcherRegex.ReplaceAllString(url.Host, "")

	// Try and open the existing hostfile in (append_only) mode
	f, err := os.OpenFile(knownHostPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		warningf("Could not open \"%s\" for reading (%s)", knownHostPath, err)
		return
	}
	defer f.Close()

	// Figure out where the ssh tools exist. On Windows, it isn't on the
	// $PATH by default, but we know where to find it.
	sshToolBinaryPath := ""
	if runtime.GOOS == "windows" {
		gitExecPathOutput, _ := b.runCommandSilentlyAndCaptureOutput("git", "--exec-path")
		if gitExecPathOutput != "" {
			sshToolBinaryPath = filepath.Join(gitExecPathOutput, "..", "..", "bin")
		}
	}

	// Grab the generated keys for the repo host
	keygenOutput, err := b.runCommandSilentlyAndCaptureOutput(filepath.Join(sshToolBinaryPath, "ssh-keygen"), "-f", knownHostPath, "-F", host)
	if err != nil {
		warningf("Could not performn `ssh-keygen` (%s)", err)
		return
	}

	// If the keygen output already contains the host, we can skip!
	if strings.Contains(keygenOutput, host) {
		commentf("Host \"%s\" already in list of known hosts at \"%s\"", host, knownHostPath)
	} else {
		// Scan the key and then write it to the known_host file
		keyscanOutput, err := b.runCommandSilentlyAndCaptureOutput(filepath.Join(sshToolBinaryPath, "ssh-keyscan"), host)
		if err != nil {
			warningf("Could not perform `ssh-keyscan` (%s)", err)
			return
		}

		if _, err = f.WriteString(keyscanOutput + "\n"); err != nil {
			warningf("Could not write to \"%s\" (%s)", knownHostPath, err)
			return
		}

		commentf("Added \"%s\" to the list of known hosts at \"%s\"", host, knownHostPath)
	}
}

// Executes a hook and applyes any environment changes. The tricky thing with
// hooks is that they can modify the ENV of a bootstrap. And it's impossible to
// grab the ENV of a child process before it finishes, so we've got an awesome
// ugly hack to get around this.  We essentially have a bash script that writes
// the ENV to a file, runs the hook, then writes the ENV back to another file.
// Once all that has finished, we compare the files, and apply what ever
// changes to our running env. Cool huh?
func (b *Bootstrap) executeHook(name string, hookPath string, exitOnError bool, env *shell.Environment) int {
	// Check if the hook exists
	if fileExists(hookPath) {
		// Create a temporary file that we'll put the hook runner code in
		tempHookRunnerFile := createTempFile(normalizeScriptFileName("buildkite-agent-bootstrap-hook-runner"))

		// Ensure the hook script is executable
		addExecutePermissiontoFile(tempHookRunnerFile.Name())

		// We'll pump the ENV before the hook into this temp file
		tempEnvBeforeFile := createTempFile("buildkite-agent-bootstrap-hook-env-before")
		tempEnvBeforeFile.Close()

		// We'll then pump the ENV _after_ the hook into this temp file
		tempEnvAfterFile := createTempFile("buildkite-agent-bootstrap-hook-env-after")
		tempEnvAfterFile.Close()

		absolutePathToHook, err := filepath.Abs(hookPath)
		if err != nil {
			exitf("Failed to find absolute path to \"%s\" (%s)", hookPath, err)
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
				"env > \"" + tempEnvBeforeFile.Name() + "\"\n" +
				". \"" + absolutePathToHook + "\"\n" +
				"BUILDKITE_LAST_HOOK_EXIT_STATUS=$?\n" +
				"env > \"" + tempEnvAfterFile.Name() + "\"\n" +
				"exit $BUILDKITE_LAST_HOOK_EXIT_STATUS"
		}

		// Write the hook script to the runner then close the file so
		// we can run it
		tempHookRunnerFile.WriteString(hookScript)
		tempHookRunnerFile.Close()

		if b.Debug {
			headerf("Preparing %s hook", name)
			commentf("A hook runner was written to \"%s\" with the following:", tempHookRunnerFile.Name())
			printf("%s", hookScript)
		}

		// Print to the screen we're going to run the hook
		headerf("Running %s hook", name)

		commentf("Executing \"%s\"", hookPath)

		// Create a copy of the current env
		previousEnv := b.env.Copy()

		// If we have a custom ENV we want to apply
		if env != nil {
			b.env = b.env.Merge(env)
		}

		// Run the hook
		hookExitStatus := b.runScript(tempHookRunnerFile.Name())

		// Restore the previous env
		b.env = previousEnv

		// Exit from the bootstrapper if the hook exited
		if exitOnError && hookExitStatus != 0 {
			errorf("The %s hook exited with a status of %d", name, hookExitStatus)
			os.Exit(hookExitStatus)
		}

		// Save the hook exit status so other hooks can get access to
		// it
		b.env.Set("BUILDKITE_LAST_HOOK_EXIT_STATUS", fmt.Sprintf("%s", hookExitStatus))

		// Compare the ENV current env with the after shots, then
		// modify the running env map with the changes.
		beforeEnv, err := shell.EnvironmentFromFile(tempEnvBeforeFile.Name())
		if err != nil {
			exitf("Failed to parse \"%s\" (%s)", tempEnvBeforeFile.Name(), err)
		}

		afterEnv, err := shell.EnvironmentFromFile(tempEnvAfterFile.Name())
		if err != nil {
			exitf("Failed to parse \"%s\" (%s)", tempEnvAfterFile.Name(), err)
		}

		// Remove the BUILDKITE_LAST_HOOK_EXIT_STATUS from the after
		// env (since we don't care about it)
		afterEnv.Remove("BUILDKITE_LAST_HOOK_EXIT_STATUS")

		diff := afterEnv.Diff(beforeEnv)
		if diff.Length() > 0 {
			headerf("Applying environment changes")
			for envDiffKey, _ := range diff.ToMap() {
				commentf("%s changed", envDiffKey)
			}
			b.env = b.env.Merge(diff)
		}

		// Apply any config changes that may have occured
		b.applyEnvironmentConfigChanges()

		return hookExitStatus
	} else {
		if b.Debug {
			headerf("Running %s hook", name)
			commentf("Skipping, no hook script found at \"%s\"", hookPath)
		}

		return 0
	}
}

// Returns the absolute path to a global hook
func (b *Bootstrap) globalHookPath(name string) string {
	return filepath.Join(b.HooksPath, normalizeScriptFileName(name))
}

// Executes a global hook
func (b *Bootstrap) executeGlobalHook(name string) int {
	return b.executeHook("global "+name, b.globalHookPath(name), true, nil)
}

// Returns the absolute path to a local hook
func (b *Bootstrap) localHookPath(name string) string {
	return filepath.Join(b.currentWorkingDirectory(), ".buildkite", "hooks", normalizeScriptFileName(name))
}

// Executes a local hook
func (b *Bootstrap) executeLocalHook(name string) int {
	return b.executeHook("local "+name, b.localHookPath(name), true, nil)
}

// Returns the absolute path to a plugin hook
func (b *Bootstrap) pluginHookPath(plugin *Plugin, name string) string {
	id, err := plugin.Identifier()
	if err != nil {
		exitf("%s", err)
	}

	dir, err := plugin.RepositorySubdirectory()
	if err != nil {
		exitf("%s", err)
	}

	return filepath.Join(b.PluginsPath, id, dir, "hooks", normalizeScriptFileName(name))
}

// Executes a plugin hook gracefully
func (b *Bootstrap) executePluginHookGracefully(plugins []*Plugin, name string) int {
	for _, p := range plugins {
		env, _ := p.ConfigurationToEnvironment()
		exitStatus := b.executeHook("plugin "+p.Label()+" "+name, b.pluginHookPath(p, name), false, env)
		if exitStatus != 0 {
			return exitStatus
		}
	}

	return 0
}

// Executes a plugin hook
func (b *Bootstrap) executePluginHook(plugins []*Plugin, name string) {
	for _, p := range plugins {
		env, _ := p.ConfigurationToEnvironment()
		b.executeHook("plugin "+p.Label()+" "+name, b.pluginHookPath(p, name), true, env)
	}
}

// If a plugin hook exists with this name
func (b *Bootstrap) pluginHookExists(plugins []*Plugin, name string) bool {
	for _, p := range plugins {
		if fileExists(b.pluginHookPath(p, name)) {
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

	if b.env.Exists("BUILDKITE_ARTIFACT_PATHS") {
		envArifactPaths := b.env.Get("BUILDKITE_ARTIFACT_PATHS")

		if envArifactPaths != b.AutomaticArtifactUploadPaths {
			b.AutomaticArtifactUploadPaths = envArifactPaths
			artifactPathsChanged = true
		}
	}

	if b.env.Exists("BUILDKITE_ARTIFACT_UPLOAD_DESTINATION") {
		envUploadDestination := b.env.Get("BUILDKITE_ARTIFACT_UPLOAD_DESTINATION")

		if envUploadDestination != b.ArtifactUploadDestination {
			b.ArtifactUploadDestination = envUploadDestination
			artifactUploadDestinationChanged = true
		}
	}

	if b.env.Exists("BUILDKITE_GIT_CLONE_FLAGS") {
		envGitCloneFlags := b.env.Get("BUILDKITE_GIT_CLONE_FLAGS")

		if envGitCloneFlags != b.GitCloneFlags {
			b.GitCloneFlags = envGitCloneFlags
			gitCloneFlagsChanged = true
		}
	}

	if b.env.Exists("BUILDKITE_GIT_CLEAN_FLAGS") {
		envGitCleanFlags := b.env.Get("BUILDKITE_GIT_CLEAN_FLAGS")

		if envGitCleanFlags != b.GitCleanFlags {
			b.GitCleanFlags = envGitCleanFlags
			gitCleanFlagsChanged = true
		}
	}

	if artifactPathsChanged || artifactUploadDestinationChanged || gitCleanFlagsChanged || gitCloneFlagsChanged {
		headerf("Bootstrap configuration has changed")

		if artifactPathsChanged {
			commentf("BUILDKITE_ARTIFACT_PATHS has been changed to \"%s\"", b.AutomaticArtifactUploadPaths)
		}

		if artifactUploadDestinationChanged {
			commentf("BUILDKITE_ARTIFACT_UPLOAD_DESTINATION has been changed to \"%s\"", b.ArtifactUploadDestination)
		}

		if gitCleanFlagsChanged {
			commentf("BUILDKITE_GIT_CLEAN_FLAGS has been changed to \"%s\"", b.GitCleanFlags)
		}

		if gitCloneFlagsChanged {
			commentf("BUILDKITE_GIT_CLONE_FLAGS has been changed to \"%s\"", b.GitCloneFlags)
		}
	}
}

func (b *Bootstrap) Start() error {
	var err error

	// Create an empty env for us to keep track of our env changes in
	b.env, _ = shell.EnvironmentFromSlice(os.Environ())

	// Add the $BUILDKITE_BIN_PATH to the $PATH if we've been given one
	if b.BinPath != "" {
		b.env.Set("PATH", fmt.Sprintf("%s%s%s", b.BinPath, string(os.PathListSeparator), b.env.Get("PATH")))
	}

	b.env.Set("BUILDKITE_BUILD_CHECKOUT_PATH", filepath.Join(b.BuildPath, dirForAgentName(b.AgentName), b.OrganizationSlug, b.PipelineSlug))

	if b.Debug {
		// Convert the env to a sorted slice
		envSlice := b.env.ToSlice()
		sort.Strings(envSlice)

		headerf("Build environment variables")
		for _, e := range envSlice {
			if strings.HasPrefix(e, "BUILDKITE") || strings.HasPrefix(e, "CI") || strings.HasPrefix(e, "PATH") {
				printf("%s", strings.Replace(e, "\n", "\\n", -1))
			}
		}
	}

	// Disable any interactive Git/SSH prompting
	b.env.Set("GIT_TERMINAL_PROMPT", "0")

	//////////////////////////////////////////////////////////////
	//
	// PLUGIN SETUP
	//
	//////////////////////////////////////////////////////////////

	var plugins []*Plugin

	if b.Plugins != "" {
		headerf("Setting up plugins")

		// Make sure we have a plugin path before trying to do anything
		if b.PluginsPath == "" {
			exitf("Can't checkout plugins without a `plugins-path`")
		}

		plugins, err = CreatePluginsFromJSON(b.Plugins)
		if err != nil {
			exitf("Failed to parse plugin definition (%s)", err)
		}

		for _, p := range plugins {
			// Get the identifer for the plugin
			id, err := p.Identifier()
			if err != nil {
				exitf("%s", err)
			}

			// Create a path to the plugin
			directory := filepath.Join(b.PluginsPath, id)
			pluginGitDirectory := filepath.Join(directory, ".git")

			// Has it already been checked out?
			if !fileExists(pluginGitDirectory) {
				// Make the directory
				err = os.MkdirAll(directory, 0777)
				if err != nil {
					exitf("%s", err)
				}

				// Try and lock this paticular plugin while we
				// check it out (we create the file outside of
				// the plugin directory so git clone doesn't
				// have a cry about the directory not being empty)
				pluginCheckoutHook, err := acquireLock(filepath.Join(b.PluginsPath, id+".lock"), 300) // Wait 5 minutes
				if err != nil {
					exitf("%s", err)
				}

				// Once we've got the lock, we need to make sure another process didn't already
				// checkout the plugin
				if fileExists(pluginGitDirectory) {
					pluginCheckoutHook.Unlock()
					commentf("Plugin \"%s\" found", p.Label())
					continue
				}

				repo, err := p.Repository()
				if err != nil {
					exitf("%s", err)
				}

				commentf("Plugin \"%s\" will be checked out to \"%s\"", p.Location, directory)

				if b.Debug {
					commentf("Checking if \"%s\" is a local repository", repo)
				}

				// Switch to the plugin directory
				previousWd := b.currentWorkingDirectory()
				b.changeWorkingDirectory(directory)

				commentf("Switching to the plugin directory")

				// If it's not a local repo, and we can perform
				// SSH fingerprint verification, do so.
				if !fileExists(repo) && b.SSHFingerprintVerification {
					b.addRepositoryHostToSSHKnownHosts(repo)
				}

				// Plugin clones shouldn't use custom GitCloneFlags
				b.runCommand("git", "clone", "-v", "--", repo, ".")

				// Switch to the version if we need to
				if p.Version != "" {
					commentf("Checking out \"%s\"", p.Version)
					b.runCommand("git", "checkout", "-f", p.Version)
				}

				// Switch back to the previous working directory
				b.changeWorkingDirectory(previousWd)

				// Now that we've succefully checked out the
				// plugin, we can remove the lock we have on
				// it.
				pluginCheckoutHook.Unlock()
			} else {
				commentf("Plugin \"%s\" found", p.Label())
			}
		}
	}

	//////////////////////////////////////////////////////////////
	//
	// ENVIRONMENT SETUP
	// A place for people to set up environment variables that
	// might be needed for their build scripts, such as secret
	// tokens and other information.
	//
	//////////////////////////////////////////////////////////////

	// The global environment hook
	b.executeGlobalHook("environment")

	// The plugin environment hook
	b.executePluginHook(plugins, "environment")

	//////////////////////////////////////////////////////////////
	//
	// REPOSITORY HANDLING
	// Creates the build directory and makes sure we're running the
	// build at the right commit.
	//
	//////////////////////////////////////////////////////////////

	// Run the `pre-checkout` global hook
	b.executeGlobalHook("pre-checkout")

	// Run the `pre-checkout` plugin hook
	b.executePluginHook(plugins, "pre-checkout")

	// Remove the checkout directory if BUILDKITE_CLEAN_CHECKOUT is present
	if b.CleanCheckout {
		headerf("Cleaning pipeline checkout")
		commentf("Removing %s", b.env.Get("BUILDKITE_BUILD_CHECKOUT_PATH"))

		err := os.RemoveAll(b.env.Get("BUILDKITE_BUILD_CHECKOUT_PATH"))
		if err != nil {
			exitf("Failed to remove \"%s\" (%s)", b.env.Get("BUILDKITE_BUILD_CHECKOUT_PATH"), err)
		}
	}

	headerf("Preparing build directory")

	// Create the build directory
	if !fileExists(b.env.Get("BUILDKITE_BUILD_CHECKOUT_PATH")) {
		commentf("Creating \"%s\"", b.env.Get("BUILDKITE_BUILD_CHECKOUT_PATH"))
		os.MkdirAll(b.env.Get("BUILDKITE_BUILD_CHECKOUT_PATH"), 0777)
	}

	// Change to the new build checkout path
	b.changeWorkingDirectory(b.env.Get("BUILDKITE_BUILD_CHECKOUT_PATH"))

	// Run a custom `checkout` hook if it's present
	if fileExists(b.globalHookPath("checkout")) {
		b.executeGlobalHook("checkout")
	} else if b.pluginHookExists(plugins, "checkout") {
		b.executePluginHook(plugins, "checkout")
	} else {
		if b.SSHFingerprintVerification {
			b.addRepositoryHostToSSHKnownHosts(b.Repository)
		}

		// Do we need to do a git checkout?
		existingGitDir := filepath.Join(b.currentWorkingDirectory(), ".git")
		if fileExists(existingGitDir) {
			// Update the the origin of the repository so we can
			// gracefully handle repository renames
			b.runCommand("git", "remote", "set-url", "origin", b.Repository)
		} else {
			gitCloneFlags, err := shlex.Split(b.GitCloneFlags)
			if err != nil {
				exitf("There was an error trying to split `%s` into arguments (%s)", b.GitCloneFlags, err)
			}

			gitCloneArguments := []string{"clone"}
			gitCloneArguments = append(gitCloneArguments, gitCloneFlags...)
			gitCloneArguments = append(gitCloneArguments, "--", b.Repository, ".")

			b.runCommand("git", gitCloneArguments...)
		}

		gitCleanFlags, err := shlex.Split(b.GitCleanFlags)
		if err != nil {
			exitf("There was an error trying to split `%s` into arguments (%s)", b.GitCleanFlags, err)
		}

		// Clean up the repository
		gitCleanRepoArguments := []string{"clean"}
		gitCleanRepoArguments = append(gitCleanRepoArguments, gitCleanFlags...)
		b.runCommand("git", gitCleanRepoArguments...)

		// Also clean up submodules if we can
		if b.GitSubmodules {
			gitCleanSubmoduleArguments := []string{"submodule", "foreach", "--recursive", "git", "clean"}
			gitCleanSubmoduleArguments = append(gitCleanSubmoduleArguments, gitCleanFlags...)

			b.runCommand("git", gitCleanSubmoduleArguments...)
		}

		// If a refspec is provided then use it instead.
		// i.e. `refs/not/a/head`
		if b.RefSpec != "" {
			commentf("Fetch and checkout custom refspec")
			b.runCommand("git", "fetch", "-v", "origin", b.RefSpec)
			b.runCommand("git", "checkout", "-f", b.Commit)

			// GitHub has a special ref which lets us fetch a pull request head, whether
			// or not there is a current head in this repository or another which
			// references the commit. We presume a commit sha is provided. See:
			// https://help.github.com/articles/checking-out-pull-requests-locally/#modifying-an-inactive-pull-request-locally
		} else if b.PullRequest != "false" && strings.Contains(b.PipelineProvider, "github") {
			commentf("Fetch and checkout pull request head")
			gitFetchExitStatus := b.runCommandGracefully("git", "fetch", "-v", "origin", "refs/pull/"+b.PullRequest+"/merge")
			if gitFetchExitStatus != 0 {
				_ = b.runCommandGracefully("git", "fetch", "-v", "origin", "refs/pull/"+b.PullRequest+"/head")
			}
			b.runCommand("git", "checkout", "-f", b.Commit)

			// If the commit is "HEAD" then we can't do a commit-specific fetch and will
			// need to fetch the remote head and checkout the fetched head explicitly.
		} else if b.Commit == "HEAD" {
			commentf("Fetch and checkout remote branch HEAD commit")
			b.runCommand("git", "fetch", "-v", "origin", b.Branch)
			b.runCommand("git", "checkout", "-f", "FETCH_HEAD")

			// Otherwise fetch and checkout the commit directly. Some repositories don't
			// support fetching a specific commit so we fall back to fetching all heads
			// and tags, hoping that the commit is included.
		} else {
			commentf("Fetch and checkout commit")
			gitFetchExitStatus := b.runCommandGracefully("git", "fetch", "-v", "origin", b.Commit)
			if gitFetchExitStatus != 0 {
				// By default `git fetch origin` will only fetch tags which are
				// reachable from a fetches branch. git 1.9.0+ changed `--tags` to
				// fetch all tags in addition to the default refspec, but pre 1.9.0 it
				// excludes the default refspec.
				gitFetchRefspec, _ := b.runCommandSilentlyAndCaptureOutput("git", "config", "remote.origin.fetch")
				b.runCommand("git", "fetch", "-v", "origin", gitFetchRefspec, "+refs/tags/*:refs/tags/*")
			}
			b.runCommand("git", "checkout", "-f", b.Commit)
		}

		if b.GitSubmodules {
			// `submodule sync` will ensure the .git/config
			// matches the .gitmodules file.  The command
			// is only available in git version 1.8.1, so
			// if the call fails, continue the bootstrap
			// script, and show an informative error.
			gitSubmoduleSyncExitStatus := b.runCommandGracefully("git", "submodule", "sync", "--recursive")
			if gitSubmoduleSyncExitStatus != 0 {
				gitVersionOutput, _ := b.runCommandSilentlyAndCaptureOutput("git", "--version")
				warningf("Failed to recursively sync git submodules. This is most likely because you have an older version of git installed (" + gitVersionOutput + ") and you need version 1.8.1 and above. If you're using submodules, it's highly recommended you upgrade if you can.")
			}

			b.runCommand("git", "submodule", "update", "--init", "--recursive", "--force")
			b.runCommand("git", "submodule", "foreach", "--recursive", "git", "reset", "--hard")
		}

		if b.env.Get("BUILDKITE_AGENT_ACCESS_TOKEN") == "" {
			warningf("Skipping sending Git information to Buildkite as $BUILDKITE_AGENT_ACCESS_TOKEN is missing")
		} else {
			// Grab author and commit information and send
			// it back to Buildkite. But before we do,
			// we'll check to see if someone else has done
			// it first.
			commentf("Checking to see if Git data needs to be sent to Buildkite")
			metaDataExistsExitStatus := b.runCommandGracefully("buildkite-agent", "meta-data", "exists", "buildkite:git:commit")
			if metaDataExistsExitStatus != 0 {
				commentf("Sending Git commit information back to Buildkite")

				gitCommitOutput, _ := b.runCommandSilentlyAndCaptureOutput("git", "show", "HEAD", "-s", "--format=fuller", "--no-color")
				gitBranchOutput, _ := b.runCommandSilentlyAndCaptureOutput("git", "branch", "--contains", "HEAD", "--no-color")

				b.runCommand("buildkite-agent", "meta-data", "set", "buildkite:git:commit", gitCommitOutput)
				b.runCommand("buildkite-agent", "meta-data", "set", "buildkite:git:branch", gitBranchOutput)
			}
		}
	}

	// Store the current value of BUILDKITE_BUILD_CHECKOUT_PATH, so we can detect if
	// one of the post-checkout hooks changed it.
	previousCheckoutPath := b.env.Get("BUILDKITE_BUILD_CHECKOUT_PATH")

	// Run the `post-checkout` global hook
	b.executeGlobalHook("post-checkout")

	// Run the `post-checkout` local hook
	b.executeLocalHook("post-checkout")

	// Run the `post-checkout` plugin hook
	b.executePluginHook(plugins, "post-checkout")

	// Capture the new checkout path so we can see if it's changed.
	newCheckoutPath := b.env.Get("BUILDKITE_BUILD_CHECKOUT_PATH")

	// If the working directory has been changed by a hook, log and switch to it
	if previousCheckoutPath != "" && previousCheckoutPath != newCheckoutPath {
		headerf("A post-checkout hook has changed the working directory to \"%s\"", newCheckoutPath)

		b.changeWorkingDirectory(newCheckoutPath)
	}

	//////////////////////////////////////////////////////////////
	//
	// RUN THE BUILD
	// Determines how to run the build, and then runs it
	//
	//////////////////////////////////////////////////////////////

	// Run the `pre-command` global hook
	b.executeGlobalHook("pre-command")

	// Run the `pre-command` local hook
	b.executeLocalHook("pre-command")

	// Run the `pre-command` plugin hook
	b.executePluginHook(plugins, "pre-command")

	var commandExitStatus int

	// Run either a custom `command` hook, or the default command runner.
	// We need to manually run these hooks so we can customize their
	// `exitOnError` behaviour
	localCommandHookPath := b.localHookPath("command")
	globalCommandHookPath := b.globalHookPath("command")

	if fileExists(localCommandHookPath) {
		commandExitStatus = b.executeHook("local command", localCommandHookPath, false, nil)
	} else if fileExists(globalCommandHookPath) {
		commandExitStatus = b.executeHook("global command", globalCommandHookPath, false, nil)
	} else if b.pluginHookExists(plugins, "command") {
		commandExitStatus = b.executePluginHookGracefully(plugins, "command")
	} else {
		// Make sure we actually have a command to run
		if b.Command == "" {
			exitf("No command has been defined. Please go to \"Pipeline Settings\" and configure your build step's \"Command\"")
		}

		pathToCommand := filepath.Join(b.currentWorkingDirectory(), strings.Replace(b.Command, "\n", "", -1))
		commandIsScript := fileExists(pathToCommand)

		// If the command isn't a script, then it's something we need
		// to eval. But before we even try running it, we should double
		// check that the agent is allowed to eval commands.
		if !commandIsScript && !b.CommandEval {
			exitf("This agent is not allowed to evaluate console commands. To allow this, re-run this agent without the `--no-command-eval` option, or specify a script within your repository to run instead (such as scripts/test.sh).")
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
							fmt.Sprintf("ECHO %s\n", windows.BatchEscape("\033[90m>\033[0m "+k)) +
							k + "\n"
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
			buildScriptPath = filepath.Join(b.currentWorkingDirectory(), normalizeScriptFileName("buildkite-script-"+b.JobID))

			if b.Debug {
				headerf("Preparing build script")
				commentf("A build script is being written to \"%s\" with the following:", buildScriptPath)
				printf("%s", buildScriptContents)
			}

			// Write the build script to disk
			err := ioutil.WriteFile(buildScriptPath, []byte(buildScriptContents), 0644)
			if err != nil {
				exitf("Failed to write to \"%s\" (%s)", buildScriptPath, err)
			}
		}

		// Ensure it can be executed
		addExecutePermissiontoFile(buildScriptPath)

		// Show we're running the script
		headerf("%s", headerLabel)
		if promptDisplay != "" {
			promptf("%s", promptDisplay)
		}

		commandExitStatus = b.runScript(buildScriptPath)
	}

	// Expand the command header if it fails
	if commandExitStatus != 0 {
		printf("^^^ +++")
	}

	// Save the command exit status to the env so hooks + plugins can access it
	b.env.Set("BUILDKITE_COMMAND_EXIT_STATUS", fmt.Sprintf("%d", commandExitStatus))

	// Run the `post-command` global hook
	b.executeGlobalHook("post-command")

	// Run the `post-command` local hook
	b.executeLocalHook("post-command")

	// Run the `post-command` plugin hook
	b.executePluginHook(plugins, "post-command")

	//////////////////////////////////////////////////////////////
	//
	// ARTIFACTS
	// Uploads and build artifacts associated with this build
	//
	//////////////////////////////////////////////////////////////

	if b.AutomaticArtifactUploadPaths != "" {
		// Run the `pre-artifact` global hook
		b.executeGlobalHook("pre-artifact")

		// Run the `pre-artifact` local hook
		b.executeLocalHook("pre-artifact")

		// Run the `pre-artifact` plugin hook
		b.executePluginHook(plugins, "pre-artifact")

		// Run the artifact upload command
		headerf("Uploading artifacts")
		artifactUploadExitStatus := b.runCommandGracefully("buildkite-agent", "artifact", "upload", b.AutomaticArtifactUploadPaths, b.ArtifactUploadDestination)

		// If the artifact upload fails, open the current group and
		// exit with an error
		if artifactUploadExitStatus != 0 {
			printf("^^^ +++")
			os.Exit(1)
		}

		// Run the `post-artifact` global hook
		b.executeGlobalHook("post-artifact")

		// Run the `post-artifact` local hook
		b.executeLocalHook("post-artifact")

		// Run the `post-artifact` plugin hook
		b.executePluginHook(plugins, "post-artifact")
	}

	// Be sure to exit this script with the same exit status that the users
	// build script exited with.
	os.Exit(commandExitStatus)

	return nil
}
