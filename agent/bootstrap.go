package agent

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"

	"github.com/buildkite/agent/env"
	"github.com/buildkite/agent/process"
)

type Bootstrap struct {
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

	// The running environment for the bootstrap file as each task runs
	env *env.Environment

	// Current working directory that shell commands get executed in
	wd string
}

var agentNameCleanupRegex = regexp.MustCompile("\"")

// Prints a line of output
func printf(format string, v ...interface{}) {
	fmt.Printf("%s\n", fmt.Sprintf(format, v...))
}

// Prints a bootstrap formatted header
func headerf(format string, v ...interface{}) {
	fmt.Printf("~~~ %s\n", fmt.Sprintf(format, v...))
}

// Shows a buildkite boostrap error
func errorf(format string, v ...interface{}) {
	headerf(":rotating_light: \033[31mBuildkite Error\033[0m")
	printf(format, v...)
}

// Shows the error text and exits the bootstrap
func fatalf(format string, v ...interface{}) {
	errorf(format, v...)
	os.Exit(1)
}

// Prints a shell prompt
func promptf(format string, v ...interface{}) {
	if runtime.GOOS == "windows" {
		fmt.Printf("^> %s\n", fmt.Sprintf(format, v...))
	} else {
		fmt.Printf("\033[90m$\033[0m %s\n", fmt.Sprintf(format, v...))
	}
}

// Will exit the bootstrap if the exit status is non successfull
func exitOnExitStatusError(exitStatus int) {
	if exitStatus != 0 {
		os.Exit(exitStatus)
	}
}

// Returns whether or not a file exists on the filesystem
func fileExists(filename string) bool {
	_, err := os.Stat(filename)

	return !os.IsNotExist(err)
}

// Come up with a nice way of showing the command if we need to
func formatCommandAndArgs(command string, args ...string) string {
	return strings.Join(append([]string{command}, args...), " ")
}

// Executes a shell function
func (b Bootstrap) shell(writer io.Writer, pty bool, command string, args ...string) int {
	// Execute the command
	c := exec.Command(command, args...)
	c.Env = b.env.ToSlice()
	c.Dir = b.wd

	formattedCommand := formatCommandAndArgs(command, args...)

	if pty {
		// Start our process in a PTY
		f, err := b.shellPTYStart(c)
		if err != nil {
			fatalf("There was an error running `%s` on a PTY (%s)", formattedCommand, err)
		}

		// Copy the pty to our buffer. This will block until it
		// EOF's or something breaks.
		_, err = io.Copy(writer, f)
		if e, ok := err.(*os.PathError); ok && e.Err == syscall.EIO {
			// We can safely ignore this error, because
			// it's just the PTY telling us that it closed
			// successfully.  See:
			// https://github.com/buildkite/agent/pull/34#issuecomment-46080419
			err = nil
		}
	} else {
		c.Stdout = writer
		c.Stderr = writer

		err := c.Start()
		if err != nil {
			fatalf("There was an error running `%s` (%s)", formattedCommand, err)
		}
	}

	// Wait for the command to finish
	waitResult := c.Wait()

	// Get the exit status
	exitStatus, err := process.GetExitStatusFromWaitResult(waitResult)
	if err != nil {
		fatalf("There was an error getting the exit status for `%s` (%s)", formattedCommand, err)
	}

	return exitStatus
}

// Runs a shell command, but prints the command to STDOUT before doing so
func (b Bootstrap) shellAndPrompt(writer io.Writer, pty bool, command string, args ...string) int {
	promptf(formatCommandAndArgs(command, args...))

	return b.shell(writer, pty, command, args...)
}

// Executes a hook and applyes any environment changes. The tricky thing with
// hooks is that they can modify the ENV of a bootstrap. And it's impossible to
// grab the ENV of a child process before it finishes, so we've got an awesome
// ugly hack to get around this.  We essentially have a bash script that writes
// the ENV to a file, runs the hook, then writes the ENV back to another file.
// Once all that has finished, we compare the files, and apply what ever
// changes to our running env. Cool huh?
func (b Bootstrap) executeHook(name string, path string, exitOnError bool) int {
	// Check if the hook exists
	if fileExists(path) {
		// Create a temporary file that we'll put the hook runner code in
		tempHookRunnerFile, err := ioutil.TempFile("", "buildkite-agent-bootstrap-hook-runner")

		// Mark the temporary hook runner file as writable
		s, err := os.Stat(tempHookRunnerFile.Name())
		if err != nil {
			fatalf("Failed to retrieve file information of `%s` as executable (%s)", tempHookRunnerFile.Name(), err)
		}
		err = os.Chmod(tempHookRunnerFile.Name(), s.Mode()|0100)
		if err != nil {
			fatalf("Failed to mark `%s` as executable (%s)", tempHookRunnerFile.Name(), err)
		}

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
			printf("A hook runner was written to \"%s\" with the following:", tempHookRunnerFile.Name())
			printf(hookScript)
		}

		// Print to the screen we're going to run the hook
		headerf("Running %s hook", name)

		// Run the hook
		hookExitStatus := b.shell(os.Stdout, b.RunInPty, tempHookRunnerFile.Name())

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
		beforeEnv, err := env.NewFromFile(tempEnvBeforeFile.Name())
		if err != nil {
			fatalf("Failed to parse \"%s\" (%s)", tempEnvBeforeFile.Name(), err)
		}

		afterEnv, err := env.NewFromFile(tempEnvAfterFile.Name())
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
					printf("%s=%s", envDiffKey, envDiffValue)
				}
			}
		}

		return hookExitStatus
	} else {
		if b.Debug {
			headerf("Running %s hook", name)
			printf("Skipping, no hook script found at: %s", path)
		}

		return 0
	}
}

// Returns the absolute path to a global hook
func (b Bootstrap) globalHookPath(name string) string {
	return path.Join(b.HooksPath, name)
}

// Executes a global hook
func (b Bootstrap) executeGlobalHook(name string) int {
	return b.executeHook("global "+name, b.globalHookPath(name), true)
}

// Returns the absolute path to a local hook
func (b Bootstrap) localHookPath(name string) string {
	return path.Join(b.wd, ".buildkite", "hooks", name)
}

// Executes a local hook
func (b Bootstrap) executeLocalHook(name string) int {
	return b.executeHook("local "+name, b.localHookPath(name), true)
}

func (b Bootstrap) Start() error {
	var exitStatus int

	// Set the working directroy
	b.wd, _ = os.Getwd()

	// Create an empty env for us to keep track of our env changes in
	b.env, _ = env.New(os.Environ())

	// Add the $BUILDKITE_BIN_PATH to the $PATH
	b.env.Set("PATH", fmt.Sprintf("%s:%s", b.BinPath, b.env.Get("PATH")))

	// Come up with the place that the repository will be checked out to
	cleanedUpAgentName := agentNameCleanupRegex.ReplaceAllString(b.AgentName, "-")
	b.env.Set("BUILDKITE_BUILD_CHECKOUT_PATH", path.Join(b.BuildPath, cleanedUpAgentName, b.ProjectSlug))

	// $ SANITIZED_AGENT_NAME=$(echo "$BUILDKITE_AGENT_NAME" | tr -d '"')
	// $ PROJECT_FOLDER_NAME="$SANITIZED_AGENT_NAME/$BUILDKITE_PROJECT_SLUG"
	// $ export BUILDKITE_BUILD_CHECKOUT_PATH="$BUILDKITE_BUILD_PATH/$PROJECT_FOLDER_NAME"

	// Show BUILDKITE_* environment variables if in debug mode. Also
	// include any custom BUILDKITE_ variables that have been added to our
	// running env map.
	if b.Debug {
		headerf("Build environment variables")
		for _, e := range b.env.ToSlice() {
			if strings.HasPrefix(e, "BUILDKITE") {
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
		printf("Removing %s", b.env.Get("BUILDKITE_BUILD_CHECKOUT_PATH"))

		err := os.RemoveAll(b.env.Get("BUILDKITE_BUILD_CHECKOUT_PATH"))
		if err != nil {
			fatalf("Failed to remove `%s` (%s)", b.env.Get("BUILDKITE_BUILD_CHECKOUT_PATH"), err)
		}
	}

	headerf("Preparing build folder")

	// Create the build directory
	printf("Creating \"%s\"", b.env.Get("BUILDKITE_BUILD_CHECKOUT_PATH"))
	os.MkdirAll(b.env.Get("BUILDKITE_BUILD_CHECKOUT_PATH"), 0777)

	// Switch the internal wd to it
	printf("Switching working directroy to build directroy")
	b.wd = b.env.Get("BUILDKITE_BUILD_CHECKOUT_PATH")

	// Run a custom `checkout` hook if it's present
	if fileExists(b.globalHookPath("checkout")) {
		b.executeGlobalHook("checkout")
	} else {
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
			exitStatus = b.shellAndPrompt(os.Stdout, false, "git", "remote", "set-url", "origin", b.Repository)
			exitOnExitStatusError(exitStatus)
		} else {
			// Does `git clone` support the --single-branch method? If it
			// does, we can use that to make first time clones faster.
			var gitCloneHelpOutput bytes.Buffer
			b.shell(&gitCloneHelpOutput, false, "git", "clone", "--help")

			// Clone the repository to the path
			if strings.Contains(gitCloneHelpOutput.String(), "--single-branch") {
				exitStatus = b.shellAndPrompt(os.Stdout, false, "git", "clone", "-qv", "--single-branch", "-b", b.Branch, "--", b.Repository, b.wd)
			} else {
				exitStatus = b.shellAndPrompt(os.Stdout, false, "git", "clone", "-qv", "--", b.Repository, b.wd)
			}
			exitOnExitStatusError(exitStatus)
		}

		// Clean up the repository
		exitStatus = b.shellAndPrompt(os.Stdout, false, "git", "clean", "-fdq")
		exitOnExitStatusError(exitStatus)

		if b.GitSubmodules {
			exitStatus = b.shellAndPrompt(os.Stdout, false, "git", "submodule", "foreach", "--recursive", "git", "clean", "-fdq")
			exitOnExitStatusError(exitStatus)
		}

		// Allow checkouts of forked pull requests on GitHub only. See:
		// https://help.github.com/articles/checking-out-pull-requests-locally/#modifying-an-inactive-pull-request-locally
		if b.PullRequest != "false" && strings.Contains(b.ProjectProvider, "github") {
			exitStatus = b.shellAndPrompt(os.Stdout, false, "git", "fetch", "origin", "+refs/pull/"+b.PullRequest+"/head:")
			exitOnExitStatusError(exitStatus)
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

			exitStatus = b.shellAndPrompt(os.Stdout, false, "git", "fetch", "origin", commitToFetch)
			if exitStatus != 0 {
				exitStatus = b.shellAndPrompt(os.Stdout, false, "git", "fetch")
				exitOnExitStatusError(exitStatus)
			}

			// Handle checking out of tags
			if b.Tag != "" {
				exitStatus = b.shellAndPrompt(os.Stdout, false, "git", "reset", "--hard", "origin/"+b.Branch)
				exitOnExitStatusError(exitStatus)
			}

			exitStatus = b.shellAndPrompt(os.Stdout, false, "git", "checkout", "-qf", b.Commit)
			exitOnExitStatusError(exitStatus)

			if b.GitSubmodules {
				//   # `submodule sync` will ensure the .git/config matches the .gitmodules file.
				//   # The command is only available in git version 1.8.1, so if the call fails,
				//   # continue the bootstrap script, and show an informative error.
				//   buildkite-prompt-and-run "git submodule sync --recursive"
				//   if [[ $? -ne 0 ]]; then
				//     buildkite-warning "Failed to recursively sync git submodules. This is most likely because you have an older version of git installed ($(git --version)) and you need version 1.8.1 and above. If you're using submodules, it's highly recommended you upgrade if you can."
				//   fi

				exitStatus = b.shellAndPrompt(os.Stdout, false, "git", "submodule", "update", "--init", "--recursive")
				exitOnExitStatusError(exitStatus)

				exitStatus = b.shellAndPrompt(os.Stdout, false, "git", "submodule", "foreach", "--recursive", "git", "reset", "--hard")
				exitOnExitStatusError(exitStatus)
			}

			// # Grab author and commit information and send it back to Buildkite
			// buildkite-debug "~~~ Saving Git information"

			// # Check to see if the meta data exists before setting it
			// buildkite-run-debug "buildkite-agent meta-data exists \"buildkite:git:commit\""
			// if [[ $? -ne 0 ]]; then
			//   buildkite-run-debug "buildkite-agent meta-data set \"buildkite:git:commit\" \"\`git show \"$BUILDKITE_COMMIT\" -s --format=fuller --no-color\`\""
			//   buildkite-run-debug "buildkite-agent meta-data set \"buildkite:git:branch\" \"\`git branch --contains \"$BUILDKITE_COMMIT\" --no-color\`\""
			// fi
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
			printf("Switching working directroy to \"%s\"", newCheckoutPathAbs)
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
		commandExitStatus = 0
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
		artifactUploadExitStatus := b.shellAndPrompt(os.Stdout, b.RunInPty, "buildkite-agent", "artifact", "upload", b.AutomaticArtifactUploadPaths, b.ArtifactUploadDestination)

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
