package agent

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"regexp"
	"runtime"
	"strings"
	"syscall"

	"github.com/buildkite/agent/process"
)

type Bootstrap struct {
	// If the bootstrap is in debug mode
	Debug bool

	// Slug of the current pipeline
	PipelineSlug string

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
	env map[string]string

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

// Returns whether or not a file exists on the filesystem
func fileExists(filename string) bool {
	_, err := os.Stat(filename)

	return !os.IsNotExist(err)
}

// Reads a file into an ENV map
func readEnvFileIntoMap(filename string) map[string]string {
	env := make(map[string]string)

	contents, err := ioutil.ReadFile(filename)
	if err != nil {
		fatalf("Error reading file: %s", err)
	}

	lines := strings.Split(string(contents), "\n")
	for _, line := range lines {
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			env[parts[0]] = parts[1]
		}
	}

	return env
}

// Turns a ENV map into a K=V slice
func convertEnvMapIntoSlice(env map[string]string) []string {
	slice := []string{}
	for k, v := range env {
		slice = append(slice, fmt.Sprintf("%s=%s", k, v))
	}
	return slice
}

// Comapres 2 env maps and returns the diff
func diffEnvMaps(beforeEnv map[string]string, afterEnv map[string]string) map[string]string {
	diff := make(map[string]string)

	for afterEnvKey, afterEnvValue := range afterEnv {
		if beforeEnv[afterEnvKey] != afterEnvValue {
			diff[afterEnvKey] = afterEnvValue
		}
	}

	return diff
}

// Come up with a nice way of showing the command if we need to
func formatCommandAndArgs(command string, args ...string) string {
	return strings.Join(append([]string{command}, args...), " ")
}

// Executes a shell function
func (b Bootstrap) shell(command string, args ...string) (int, string) {
	// Execute the command
	c := exec.Command(command, args...)
	c.Env = append(os.Environ(), convertEnvMapIntoSlice(b.env)...)
	c.Dir = b.wd

	// A buffer and multi writer so we can capture shell output
	var buffer bytes.Buffer
	multiWriter := io.MultiWriter(&buffer, os.Stdout)

	formattedCommand := formatCommandAndArgs(command, args...)

	if b.RunInPty {
		// Start our process in a PTY
		f, err := b.shellPTYStart(c)
		if err != nil {
			fatalf("There was an error running `%s` on a PTY (%s)", formattedCommand, err)
		}

		// Copy the pty to our buffer. This will block until it
		// EOF's or something breaks.
		_, err = io.Copy(multiWriter, f)
		if e, ok := err.(*os.PathError); ok && e.Err == syscall.EIO {
			// We can safely ignore this error, because
			// it's just the PTY telling us that it closed
			// successfully.  See:
			// https://github.com/buildkite/agent/pull/34#issuecomment-46080419
			err = nil
		}
	} else {
		c.Stdout = multiWriter
		c.Stderr = multiWriter

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

	return exitStatus, buffer.String()
}

// Runs a shell command, but prints the command to STDOUT before doing so
func (b Bootstrap) shellAndPrompt(command string, args ...string) (int, string) {
	promptf(formatCommandAndArgs(command, args...))
	return b.shell(command, args...)
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
		defer tempHookRunnerFile.Close()

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
		defer tempEnvBeforeFile.Close()

		// We'll then pump the ENV _after_ the hook into this temp file
		tempEnvAfterFile, err := ioutil.TempFile("", "buildkite-agent-bootstrap-hook-env-after")
		defer tempEnvAfterFile.Close()

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

		// Write the hook script to the runner
		tempHookRunnerFile.WriteString(hookScript)
		tempHookRunnerFile.Sync()

		if b.Debug {
			headerf("Preparing %s hook", name)
			printf("A hook runner was written to \"%s\" with the following:", tempHookRunnerFile.Name())
			printf(hookScript)
		}

		// Print to the screen we're going to run the hook
		headerf("Running %s hook", name)

		// Run the hook
		hookExitStatus, _ := b.shell(tempHookRunnerFile.Name())

		// Exit from the bootstrapper if the hook exited
		if exitOnError && hookExitStatus != 0 {
			errorf("The %s hook exited with a status of %d", name, hookExitStatus)
			os.Exit(hookExitStatus)
		}

		// Save the hook exit status so other hooks can get access to
		// it
		b.env["BUILDKITE_LAST_HOOK_EXIT_STATUS"] = fmt.Sprintf("%s", hookExitStatus)

		// Compare the ENV current env with the after shots, then
		// modify the running env map with the changes.
		envDiff := diffEnvMaps(readEnvFileIntoMap(tempEnvBeforeFile.Name()), readEnvFileIntoMap(tempEnvAfterFile.Name()))
		if len(envDiff) > 0 {
			if b.Debug {
				headerf("Applying environment changes")
			}
			for envDiffKey, envDiffValue := range envDiff {
				b.env[envDiffKey] = envDiffValue
				if b.Debug {
					printf("%s=%s was added/changed to the environment", envDiffKey, envDiffValue)
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

// Checkouts source code a paticular revision on the file system. It will do a
// fresh clone if it doesn't exist, or update an existing repository if it
// does.
func (b Bootstrap) checkoutRepository(repository string, revision string) {
	// TODO
}

func (b Bootstrap) Start() error {
	// Set the working directroy
	b.wd, _ = os.Getwd()

	// Create an empty env for us to keep track of our env changes in
	b.env = make(map[string]string)

	// Add the $BUILDKITE_BIN_PATH to the $PATH
	b.env["PATH"] = fmt.Sprintf("%s:%s", b.BinPath, os.Getenv("PATH"))

	// Come up with the place that the repository will be checked out to
	cleanedUpAgentName := agentNameCleanupRegex.ReplaceAllString(b.AgentName, "-")
	b.env["BUILDKITE_BUILD_CHECKOUT_PATH"] = path.Join(b.BuildPath, cleanedUpAgentName, b.PipelineSlug)

	// $ SANITIZED_AGENT_NAME=$(echo "$BUILDKITE_AGENT_NAME" | tr -d '"')
	// $ PROJECT_FOLDER_NAME="$SANITIZED_AGENT_NAME/$BUILDKITE_PROJECT_SLUG"
	// $ export BUILDKITE_BUILD_CHECKOUT_PATH="$BUILDKITE_BUILD_PATH/$PROJECT_FOLDER_NAME"

	// Show BUILDKITE_* environment variables if in debug mode. Also
	// include any custom BUILDKITE_ variables that have been added to our
	// running env map.
	if b.Debug {
		headerf("Build environment variables")
		for _, e := range append(convertEnvMapIntoSlice(b.env), os.Environ()...) {
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
		printf("Removing %s", b.env["BUILDKITE_BUILD_CHECKOUT_PATH"])

		err := os.RemoveAll(b.env["BUILDKITE_BUILD_CHECKOUT_PATH"])
		if err != nil {
			fatalf("Failed to remove `%s` (%s)", b.env["BUILDKITE_BUILD_CHECKOUT_PATH"], err)
		}
	}

	headerf("Preparing build folder")

	// Create the build directory
	printf("Creating %s", b.env["BUILDKITE_BUILD_CHECKOUT_PATH"])
	os.MkdirAll(b.env["BUILDKITE_BUILD_CHECKOUT_PATH"], 0777)

	// Switch the internal wd to it
	printf("Switching working directroy to build directroy")
	b.wd = b.env["BUILDKITE_BUILD_CHECKOUT_PATH"]

	if fileExists(b.globalHookPath("checkout")) {
		b.executeGlobalHook("checkout")
	} else {
		b.checkoutRepository("", "")
	}

	// Store the current value of BUILDKITE_BUILD_CHECKOUT_PATH, so we can detect if
	// one of the post-checkout hooks changed it.
	previousCheckoutPath := b.env["BUILDKITE_BUILD_CHECKOUT_PATH"]

	// Run the `post-checkout` global hook
	b.executeGlobalHook("post-checkout")

	// Run the `post-checkout` local hook
	b.executeLocalHook("post-checkout")

	// Capture the new checkout path so we can see if it's changed
	newCheckoutPath := b.env["BUILDKITE_BUILD_CHECKOUT_PATH"]

	// If the working directory has been changed by a hook, log and switch to it
	if b.wd != "" && previousCheckoutPath != newCheckoutPath {
		headerf("A post-checkout hook has changed the working directory to %s", newCheckoutPath)

		if fileExists(newCheckoutPath) {
			b.wd = newCheckoutPath
		} else {
			fatalf("Failed to switch to \"%s\" as it doesn't exist", newCheckoutPath)
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

	// TODO
	commandExitStatus := 0
	b.env["BUILDKITE_COMMAND_EXIT_STATUS"] = fmt.Sprintf("%d", commandExitStatus)

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
		artifactUploadExitStatus, _ := b.shellAndPrompt("buildkite-agent", "artifact", "upload", b.AutomaticArtifactUploadPaths, b.ArtifactUploadDestination)

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
