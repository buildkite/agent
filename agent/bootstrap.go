package agent

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/buildkite/agent/shell"
	"github.com/buildkite/agent/vendor/src/github.com/mitchellh/go-homedir"
	"github.com/buildkite/agent/vendor/src/github.com/nightlyone/lockfile"
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

	// Should git submodules be checked out
	GitSubmodules bool

	// If the commit was part of a pull request, this will container the PR number
	PullRequest string

	// The provider of the the project
	ProjectProvider string

	// Slug of the current project
	ProjectSlug string

	// Name of the agent running the bootstrap
	AgentName string

	// Should the bootstrap remove an existing checkout before running the job
	CleanCheckout bool

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

	// Paths to automatically upload as artifacts when the build finishes
	AutomaticArtifactUploadPaths string

	// A custom destination to upload artifacts to (i.e. s3://...)
	ArtifactUploadDestination string

	// Whether or not to automatically authorize SSH key hosts
	AutoSSHFingerprintVerification bool

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
	headerf(":rotating_light: \033[31mBuildkite Error\033[0m")
	printf(format, v...)
}

// Shows a buildkite boostrap warning
func warningf(format string, v ...interface{}) {
	printf("\033[33m⚠️ Buildkite Warning: %s\033[0m", fmt.Sprintf(format, v...))
	printf("^^^ +++")
}

// Shows the error text and exits the bootstrap
func fatalf(format string, v ...interface{}) {
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

// Returns whether or not a file exists on the filesystem
func fileExists(filename string) bool {
	_, err := os.Stat(filename)

	return !os.IsNotExist(err)
}

// Changes the permission of a file so it can be executed
func addExecutePermissiontoFile(filename string) {
	s, err := os.Stat(filename)
	if err != nil {
		fatalf("Failed to retrieve file information of \"%s\": %s", filename, err)
	}

	err = os.Chmod(filename, s.Mode()|0100)
	if err != nil {
		fatalf("Failed to mark \"%s\" as executable: %s", filename, err)
	}
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
		fatalf("There was error running `%s`: %s", cmd, err)
	}
}

// Creates a shell command ready for running
func (b *Bootstrap) newCommand(command string, args ...string) *shell.Command {
	return &shell.Command{Command: command, Args: args, Env: b.env, Dir: b.wd}
}

// Run a command without showing a prompt or the output to the user
func (b *Bootstrap) runCommandSilentlyAndCaptureOutput(command string, args ...string) string {
	cmd := b.newCommand(command, args...)

	var buffer bytes.Buffer
	shell.Run(cmd, &shell.Config{Writer: &buffer})

	return strings.TrimSpace(buffer.String())
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
	cmd := b.newCommand(command)

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
		warningf("Could not find the current users home directory: %s", err)
		return
	}

	// Construct paths to the known_hosts file
	sshDirectory := path.Join(userHomePath, ".ssh")
	knownHostPath := path.Join(sshDirectory, "known_hosts")

	// Ensure a directory exists and known_host file exist
	if !fileExists(knownHostPath) {
		os.MkdirAll(sshDirectory, 0755)
		ioutil.WriteFile(knownHostPath, []byte(""), 0644)
	}

	// Create a lock on the known_host file so other agents don't try and
	// change it at the same time
	knownHostLockPath := path.Join(sshDirectory, "known_hosts.lock")
	knownHostLock, _ := lockfile.New(path.Join(sshDirectory, "known_hosts.lock"))

	// Aquire the known host lock. Keep trying the lock until we get it
	attempts := 0
	for true {
		err = knownHostLock.TryLock()
		if err != nil {
			// Keey track of how many times we tried to get the
			// lock
			attempts += 1
			if attempts > 10 {
				warningf("Gave up trying to aquire a lock on \"%s\"", knownHostLockPath)
				return
			}

			// Try again in 100 ms
			time.Sleep(1 * time.Second)
		} else {
			break
		}
	}

	// Unlock the known_host lock when we're done
	defer knownHostLock.Unlock()

	// Clean up the SSH host and remove any key identifiers. See:
	// git@github.com-custom-identifier:foo/bar.git
	// https://buildkite.com/docs/agent/ssh-keys#creating-multiple-ssh-keys
	var repoSSHKeySwitcherRegex = regexp.MustCompile(`-[a-z0-9\-]+$`)
	host := repoSSHKeySwitcherRegex.ReplaceAllString(url.Host, "")

	// Try and open the existing hostfile in (append_only) mode
	f, err := os.OpenFile(knownHostPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		warningf("Could not open \"%s\" for reading: %s", knownHostPath, err)
		return
	}
	defer f.Close()

	// Figure out where the ssh tools exist
	sshToolBinaryPath := ""

	// Grab the generated keys for the repo host
	keygenOutput := b.runCommandSilentlyAndCaptureOutput(path.Join(sshToolBinaryPath, "ssh-keygen"), "-f", knownHostPath, "-F", host)

	// If the keygen output doesn't contain the host, we can skip!
	if !strings.Contains(keygenOutput, host) {
		// Scan the key and then write it to the known_host file
		keyscanOutput := b.runCommandSilentlyAndCaptureOutput(path.Join(sshToolBinaryPath, "ssh-keyscan"), host)

		if _, err = f.WriteString(keyscanOutput + "\n"); err != nil {
			warningf("Could not write to \"%s\": %s", knownHostPath, err)
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
func (b *Bootstrap) executeHook(name string, path string, exitOnError bool) int {
	// Check if the hook exists
	if fileExists(path) {
		// Create a temporary file that we'll put the hook runner code in
		tempHookRunnerFile, err := ioutil.TempFile("", "buildkite-agent-bootstrap-hook-runner")

		// Ensure the hook script is executable
		addExecutePermissiontoFile(tempHookRunnerFile.Name())

		// We'll pump the ENV before the hook into this temp file
		tempEnvBeforeFile, err := ioutil.TempFile("", "buildkite-agent-bootstrap-hook-env-before")
		tempEnvBeforeFile.Close()

		// We'll then pump the ENV _after_ the hook into this temp file
		tempEnvAfterFile, err := ioutil.TempFile("", "buildkite-agent-bootstrap-hook-env-after")
		tempEnvAfterFile.Close()

		// Create the hook runner code
		var hookScript string
		if runtime.GOOS == "windows" {
			hookScript = "SET > \"" + tempEnvBeforeFile.Name() + "\"\n" +
				"call \"" + path + "\"\n" +
				"BUILDKITE_LAST_HOOK_EXIT_STATUS=!ERRORLEVEL!\n" +
				"SET > \"" + tempEnvAfterFile.Name() + "\"\n" +
				"EXIT %BUILDKITE_LAST_HOOK_EXIT_STATUS%"
		} else {
			hookScript = "#!/bin/bash\n" +
				"env > \"" + tempEnvBeforeFile.Name() + "\"\n" +
				". \"" + path + "\"\n" +
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
			printf(hookScript)
		}

		// Print to the screen we're going to run the hook
		headerf("Running %s hook", name)

		commentf("Executing \"%s\"", path)

		// Run the hook
		hookExitStatus := b.runScript(tempHookRunnerFile.Name())

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
			fatalf("Failed to parse \"%s\" (%s)", tempEnvBeforeFile.Name(), err)
		}

		afterEnv, err := shell.EnvironmentFromFile(tempEnvAfterFile.Name())
		if err != nil {
			fatalf("Failed to parse \"%s\" (%s)", tempEnvAfterFile.Name(), err)
		}

		diff := afterEnv.Diff(beforeEnv)
		if diff.Length() > 0 {
			if b.Debug {
				headerf("Applying environment changes")
			}
			for envDiffKey, envDiffValue := range diff.ToMap() {
				b.env.Set(envDiffKey, envDiffValue)
				if b.Debug {
					commentf("%s=%s", envDiffKey, envDiffValue)
				}
			}
		}

		return hookExitStatus
	} else {
		if b.Debug {
			headerf("Running %s hook", name)
			commentf("Skipping, no hook script found at \"%s\"", path)
		}

		return 0
	}
}

// Returns the absolute path to a global hook
func (b *Bootstrap) globalHookPath(name string) string {
	return path.Join(b.HooksPath, name)
}

// Executes a global hook
func (b *Bootstrap) executeGlobalHook(name string) int {
	return b.executeHook("global "+name, b.globalHookPath(name), true)
}

// Returns the absolute path to a local hook
func (b *Bootstrap) localHookPath(name string) string {
	return path.Join(b.wd, ".buildkite", "hooks", name)
}

// Executes a local hook
func (b *Bootstrap) executeLocalHook(name string) int {
	return b.executeHook("local "+name, b.localHookPath(name), true)
}

func (b *Bootstrap) Start() error {
	// Create an empty env for us to keep track of our env changes in
	b.env, _ = shell.EnvironmentFromSlice(os.Environ())

	// Add the $BUILDKITE_BIN_PATH to the $PATH
	b.env.Set(shell.ENVIRONMENT_KEY_PATH, fmt.Sprintf("%s:%s", b.BinPath, b.env.Get(shell.ENVIRONMENT_KEY_PATH)))

	// Come up with the place that the repository will be checked out to
	var agentNameCleanupRegex = regexp.MustCompile("\"")
	cleanedUpAgentName := agentNameCleanupRegex.ReplaceAllString(b.AgentName, "-")

	b.wd = b.env.Set("BUILDKITE_BUILD_CHECKOUT_PATH", path.Join(b.BuildPath, cleanedUpAgentName, b.ProjectSlug))

	if b.Debug {
		// Convert the env to a sorted slice
		envSlice := b.env.ToSlice()
		sort.Strings(envSlice)

		headerf("Build environment variables")
		for _, e := range envSlice {
			if strings.HasPrefix(e, "BUILDKITE") || strings.HasPrefix(e, "CI") || strings.HasPrefix(e, shell.ENVIRONMENT_KEY_PATH) {
				printf(e)
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

	//////////////////////////////////////////////////////////////
	//
	// REPOSITORY HANDLING
	// Creates the build folder and makes sure we're running the
	// build at the right commit.
	//
	//////////////////////////////////////////////////////////////

	// Run the `pre-checkout` global hook
	b.executeGlobalHook("pre-checkout")

	// Remove the checkout folder if BUILDKITE_CLEAN_CHECKOUT is present
	if b.CleanCheckout {
		headerf("Cleaning project checkout")
		commentf("Removing %s", b.env.Get("BUILDKITE_BUILD_CHECKOUT_PATH"))

		err := os.RemoveAll(b.env.Get("BUILDKITE_BUILD_CHECKOUT_PATH"))
		if err != nil {
			fatalf("Failed to remove \"%s\": %s", b.env.Get("BUILDKITE_BUILD_CHECKOUT_PATH"), err)
		}
	}

	headerf("Preparing build folder")

	// Create the build directory
	commentf("Creating \"%s\"", b.env.Get("BUILDKITE_BUILD_CHECKOUT_PATH"))
	os.MkdirAll(b.env.Get("BUILDKITE_BUILD_CHECKOUT_PATH"), 0777)

	// Switch the internal wd to it
	commentf("Switching working directroy to build folder")
	b.wd = b.env.Get("BUILDKITE_BUILD_CHECKOUT_PATH")

	// Run a custom `checkout` hook if it's present
	if fileExists(b.globalHookPath("checkout")) {
		b.executeGlobalHook("checkout")
	} else {
		if b.AutoSSHFingerprintVerification {
			b.addRepositoryHostToSSHKnownHosts(b.Repository)
		}

		// If enabled, automatically run an ssh-keyscan on the git ssh host, to prevent
		// a yes/no promp from appearing when cloning/fetching
		// if [[ ! -z "${BUILDKITE_AUTO_SSH_FINGERPRINT_VERIFICATION:-}" ]] && [[ "$BUILDKITE_AUTO_SSH_FINGERPRINT_VERIFICATION" == "true" ]]; then
		//   # Only bother running the keyscan if the SSH host has been provided by
		//   # Buildkite. It won't be present if the host isn't using the SSH protocol
		//   if [[ ! -z "${BUILDKITE_REPO_SSH_HOST:-}" ]]; then
		//     : "${BUILDKITE_SSH_DIRECTORY:="$HOME/.ssh"}"
		//     : "${BUILDKITE_SSH_KNOWN_HOST_PATH:="$BUILDKITE_SSH_DIRECTORY/known_hosts"}"

		//     # Ensure the known_hosts file exists
		//     mkdir -p "$BUILDKITE_SSH_DIRECTORY"
		//     touch "$BUILDKITE_SSH_KNOWN_HOST_PATH"

		//     # Only add the output from ssh-keyscan if it doesn't already exist in the
		//     # known_hosts file
		//     if ! ssh-keygen -H -F "$BUILDKITE_REPO_SSH_HOST" | grep -q "$BUILDKITE_REPO_SSH_HOST"; then
		//       buildkite-run "ssh-keyscan \"$BUILDKITE_REPO_SSH_HOST\" >> \"$BUILDKITE_SSH_KNOWN_HOST_PATH\""
		//     fi
		//   fi
		// fi

		// Disable any interactive Git/SSH prompting
		b.env.Set("GIT_TERMINAL_PROMPT", "0")

		// Do we need to do a git checkout?
		existingGitDir := path.Join(b.wd, ".git")
		if fileExists(existingGitDir) {
			// Update the the origin of the repository so we can
			// gracefully handle repository renames
			b.runCommand("git", "remote", "set-url", "origin", b.Repository)
		} else {
			// Does `git clone` support the --single-branch method?
			// If it does, we can use that to make first time
			// clones faster.
			gitCloneHelpOutput := b.runCommandSilentlyAndCaptureOutput("git", "clone", "--help")

			// Clone the repository to the path
			if strings.Contains(gitCloneHelpOutput, "--single-branch") {
				b.runCommand("git", "clone", "-qv", "--single-branch", "-b", b.Branch, "--", b.Repository, b.wd)
			} else {
				b.runCommand("git", "clone", "-qv", "--", b.Repository, b.wd)
			}
		}

		// Clean up the repository
		b.runCommand("git", "clean", "-fdq")

		// Also clean up submodules if we can
		if b.GitSubmodules {
			b.runCommand("git", "submodule", "foreach", "--recursive", "git", "clean", "-fdq")
		}

		// Allow checkouts of forked pull requests on GitHub only. See:
		// https://help.github.com/articles/checking-out-pull-requests-locally/#modifying-an-inactive-pull-request-locally
		if b.PullRequest != "false" && strings.Contains(b.ProjectProvider, "github") {
			b.runCommand("git", "fetch", "-q", "origin", "+refs/pull/"+b.PullRequest+"/head:")
		} else {
			// If the commit is HEAD, we can't do a commit-only
			// fetch, we'll need to use the branch instead.  During
			// the fetch, we do first try and grab the commit only
			// (because it's usually much faster).  If that doesn't
			// work, just resort back to a regular fetch.
			var commitToFetch string
			if b.Commit == "HEAD" {
				commitToFetch = b.Branch
			} else {
				commitToFetch = b.Commit
			}

			gitFetchExitStatus := b.runCommandGracefully("git", "fetch", "-q", "origin", commitToFetch)
			if gitFetchExitStatus != 0 {
				b.runCommand("git", "fetch", "-q")
			}

			// Handle checking out of tags
			if b.Tag != "" {
				b.runCommand("git", "reset", "--hard", "origin/"+b.Branch)
			}

			b.runCommand("git", "checkout", "-qf", b.Commit)

			if b.GitSubmodules {
				// `submodule sync` will ensure the .git/config
				// matches the .gitmodules file.  The command
				// is only available in git version 1.8.1, so
				// if the call fails, continue the bootstrap
				// script, and show an informative error.
				gitSubmoduleSyncExitStatus := b.runCommandGracefully("git", "submodule", "sync", "--recursive")
				if gitSubmoduleSyncExitStatus != 0 {
					gitVersionOutput := b.runCommandSilentlyAndCaptureOutput("git", "--version")
					warningf("Failed to recursively sync git submodules. This is most likely because you have an older version of git installed (" + gitVersionOutput + ") and you need version 1.8.1 and above. If you're using submodules, it's highly recommended you upgrade if you can.")
				}

				b.runCommand("git", "submodule", "update", "--init", "--recursive")
				b.runCommand("git", "submodule", "foreach", "--recursive", "git", "reset", "--hard")
			}

			// Grab author and commit information and send it back to Buildkite. But before we do, we'll
			// check to see if someone else has done it first.
			commentf("Checking to see if Git data needs to be sent to Buildkite")
			metaDataExistsExitStatus := b.runCommandGracefully("buildkite-agent", "meta-data", "exists", "buildkite:git:commit")
			if metaDataExistsExitStatus != 0 {
				commentf("Sending Git commit information back to Buildkite")

				gitCommitOutput := b.runCommandSilentlyAndCaptureOutput("git", "show", b.Commit, "-s", "--format=fuller", "--no-color")
				gitBranchOutput := b.runCommandSilentlyAndCaptureOutput("git", "branch", "--contains", b.Commit, "--no-color")

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

	// Capture the new checkout path so we can see if it's changed. We need
	// to also handle the case where they just switch it to "foo/bar",
	// because that directroy is relative to the current working directroy.
	newCheckoutPath := b.env.Get("BUILDKITE_BUILD_CHECKOUT_PATH")
	newCheckoutPathAbs := newCheckoutPath
	if !filepath.IsAbs(newCheckoutPathAbs) {
		newCheckoutPathAbs = path.Join(b.wd, newCheckoutPath)
	}

	// If the working directory has been changed by a hook, log and switch to it
	if b.wd != "" && previousCheckoutPath != newCheckoutPathAbs {
		headerf("A post-checkout hook has changed the working directory to \"%s\"", newCheckoutPath)

		if fileExists(newCheckoutPathAbs) {
			commentf("Switching working directroy to \"%s\"", newCheckoutPathAbs)
			b.wd = newCheckoutPathAbs
		} else {
			fatalf("Failed to switch to \"%s\" as it doesn't exist", newCheckoutPathAbs)
		}
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

	var commandExitStatus int

	// Run either a custom `command` hook, or the default command runner.
	// We need to manually run these hooks so we can customize their
	// `exitOnError` behaviour
	localCommandHookPath := b.localHookPath("command")
	globalCommandHookPath := b.localHookPath("command")

	if fileExists(localCommandHookPath) {
		commandExitStatus = b.executeHook("local command", localCommandHookPath, false)
	} else if fileExists(globalCommandHookPath) {
		commandExitStatus = b.executeHook("global command", globalCommandHookPath, false)
	} else {
		// Make sure we actually have a command to run
		if b.Command == "" {
			fatalf("No command has been defined. Please go to \"Project Settings\" and configure your build step's \"Command\"")
		}

		pathToCommand := path.Join(b.wd, b.Command)

		// If the command isn't a file on the filesystem, then it's
		// something we need to eval. But before we even try running
		// it, we should double check that the agent is allowed to eval
		// commands.
		if !fileExists(pathToCommand) && !b.CommandEval {
			fatalf("This agent is not allowed to evaluate console commands. To allow this, re-run this agent without the `--no-command-eval` option, or specify a script within your repository to run instead (such as scripts/test.sh).")
		}

		if b.Debug {
			headerf("Preparing build script")
		}

		// Come up with the contents of the build script. While we
		// generate the script, we need to handle the case of running a
		// script vs. a command differently
		var buildScript string
		var promptDisplay string
		var headerLabel string
		if runtime.GOOS == "windows" {
			if fileExists(pathToCommand) {
				promptDisplay = "CALL " + b.Command
				headerLabel = "Running build script"
				buildScript = "ECHO hello"
			} else {
				promptDisplay = b.Command
				headerLabel = "Running command"
				buildScript = "#!/bin/bash\n" + b.Command
			}
		} else {
			if fileExists(pathToCommand) {
				promptDisplay = "./\"" + b.Command + "\""
				headerLabel = "Running build script"
				buildScript = "#!/bin/bash\nchmod +x \"" + b.Command + "\"\n./\"" + b.Command + "\""
			} else {
				promptDisplay = b.Command
				headerLabel = "Running command"
				buildScript = "#!/bin/bash\n" + b.Command
			}
		}

		buildScriptPath := path.Join(b.wd, "buildkite-script-"+b.JobID)

		// Write the build script to disk
		err := ioutil.WriteFile(buildScriptPath, []byte(buildScript), 0644)
		if err != nil {
			fatalf("Failed to write to \"%s\": %s", buildScriptPath, err)
		}

		// Ensure it can be executed
		addExecutePermissiontoFile(buildScriptPath)

		if b.Debug {
			commentf("A build script written to \"%s\" with the following:", buildScriptPath)
			printf(buildScript)
		}

		headerf(headerLabel)
		promptf(promptDisplay)

		commandExitStatus = b.runScript(buildScriptPath)
	}

	// Save the command exit status to the env so hooks + plugins can access it
	b.env.Set("BUILDKITE_COMMAND_EXIT_STATUS", fmt.Sprintf("%d", commandExitStatus))

	// Run the `post-command` global hook
	b.executeGlobalHook("post-command")

	// Run the `post-command` local hook
	b.executeLocalHook("post-command")

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

	}

	// Be sure to exit this script with the same exit status that the users
	// build script exited with.
	os.Exit(commandExitStatus)

	return nil
}
