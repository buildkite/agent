package bootstrap

import (
	"fmt"
	"io/ioutil"
	"path/filepath"
	"runtime"

	"github.com/buildkite/agent/bootstrap/shell"
	"github.com/buildkite/agent/env"
)

// Hook is an executable that is "sourced" into the bootstrap. It gets passed
// the bootstrap environment and any environment variables that it exports
// get applied to the bootstrap.
//
// The tricky thing is that it's impossible to grab the ENV of a child process
// before it finishes, so we've got an awesome (ugly) hack to get around this.
// We write the ENV to file, run the hook and then write the ENV back to another file.
// Then we can use the diff of the two to figure out what changes to make to the
// bootstrap. Horrible, but effective.
type Hook struct {
	Name        string
	Path        string
	ExitOnError bool
	Env         *env.Environment
	Shell       *shell.Shell
	Debug       bool
}

func (h *Hook) Execute() (*env.Environment, error) {
	if !fileExists(h.Path) {
		if h.Debug {
			h.Shell.Headerf("Running %s hook", h.Name)
			h.Shell.Commentf("Skipping, no hook script found at \"%s\"", h.Path)
		}
		return nil, nil
	}

	// Create a temporary file that we'll put the hook runner code in
	tempHookRunnerFile, err := shell.TempFileWithExtension(normalizeScriptFileName("buildkite-agent-bootstrap-hook-runner"))
	if err != nil {
		return nil, err
	}

	// Ensure the hook script is executable
	if err := addExecutePermissiontoFile(tempHookRunnerFile.Name()); err != nil {
		return nil, err
	}

	// We'll pump the ENV before the hook into this temp file
	tempEnvBeforeFile, err := shell.TempFileWithExtension("buildkite-agent-bootstrap-hook-env-before")
	if err != nil {
		return nil, err
	}
	tempEnvBeforeFile.Close()

	// We'll then pump the ENV _after_ the hook into this temp file
	tempEnvAfterFile, err := shell.TempFileWithExtension("buildkite-agent-bootstrap-hook-env-after")
	if err != nil {
		return nil, err
	}
	tempEnvAfterFile.Close()

	absolutePathToHook, err := filepath.Abs(h.Path)
	if err != nil {
		return nil, fmt.Errorf("Failed to find absolute path to \"%s\" (%s)", h.Path, err)
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

	if h.Debug {
		h.Shell.Headerf("Preparing %s hook", h.Name)
		h.Shell.Commentf("A hook runner was written to \"%s\" with the following:", tempHookRunnerFile.Name())
		h.Shell.Printf("%s", hookScript)
	}

	// Print to the screen we're going to run the hook
	h.Shell.Headerf("Running %s hook", h.Name)
	h.Shell.Commentf("Executing \"%s\"", h.Path)

	// Create a copy of the current env
	previousEnviron := h.Shell.Env.Copy()

	// Apply our environment
	h.Shell.Env = h.Env

	// Run the hook
	hookErr := h.Shell.RunScript(tempHookRunnerFile.Name())

	// Restore the previous env
	h.Shell.Env = previousEnviron

	// Exit from the bootstrapper if the hook exited
	if h.ExitOnError && hookErr != nil {
		h.Shell.Errorf("The %s hook exited with an error: %v", h.Name, hookErr)
		return nil, err
	}

	// Save the hook exit status so other hooks can get access to it
	h.Shell.Env.Set("BUILDKITE_LAST_HOOK_EXIT_STATUS", fmt.Sprintf("%d", shell.GetExitCode(hookErr)))

	var beforeEnv *env.Environment
	var afterEnv *env.Environment

	// Compare the ENV current env with the after shots
	beforeEnvContents, err := ioutil.ReadFile(tempEnvBeforeFile.Name())
	if err != nil {
		return nil, fmt.Errorf("Failed to read \"%s\" (%s)", tempEnvBeforeFile.Name(), err)
	}
	beforeEnv = env.FromExport(string(beforeEnvContents))

	afterEnvContents, err := ioutil.ReadFile(tempEnvAfterFile.Name())
	if err != nil {
		return nil, fmt.Errorf("Failed to read \"%s\" (%s)", tempEnvAfterFile.Name(), err)
	}
	afterEnv = env.FromExport(string(afterEnvContents))

	// Remove the BUILDKITE_LAST_HOOK_EXIT_STATUS from the after
	// env (since we don't care about it)
	afterEnv.Remove("BUILDKITE_LAST_HOOK_EXIT_STATUS")

	return afterEnv.Diff(beforeEnv), hookErr
}
