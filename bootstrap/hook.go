package bootstrap

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"

	"github.com/buildkite/agent/bootstrap/shell"
	"github.com/buildkite/agent/env"
)

// Hooks get "sourced" into the bootstrap in the sense that they get the
// environment set for them and then we capture any extra environment variables
// that are exported in the script.
//
// The tricky thing is that it's impossible to grab the ENV of a child process
// before it finishes, so we've got an awesome (ugly) hack to get around this.
// We write the ENV to file, run the hook and then write the ENV back to another file.
// Then we can use the diff of the two to figure out what changes to make to the
// bootstrap. Horrible, but effective.

type hookRunner struct {
	hookPath      string
	scriptFile    *os.File
	beforeEnvFile *os.File
	afterEnvFile  *os.File
}

func newHookScript(hookPath string) *hookRunner {
	return &hookRunner{
		hookPath: hookPath,
	}
}

func (h *hookRunner) Run() error {
	var err error

	// Create a temporary file that we'll put the hook runner code in
	h.scriptFile, err = shell.TempFileWithExtension(normalizeScriptFileName(
		`buildkite-agent-bootstrap-hook-runner`,
	))
	if err != nil {
		return err
	}

	// We'll pump the ENV before the hook into this temp file
	h.beforeEnvFile, err = shell.TempFileWithExtension(
		`buildkite-agent-bootstrap-hook-env-before`,
	)
	if err != nil {
		return err
	}
	h.beforeEnvFile.Close()

	// We'll then pump the ENV _after_ the hook into this temp file
	h.afterEnvFile, err = shell.TempFileWithExtension(
		`buildkite-agent-bootstrap-hook-env-after`,
	)
	if err != nil {
		return err
	}
	h.afterEnvFile.Close()

	absolutePathToHook, err := filepath.Abs(h.hookPath)
	if err != nil {
		return fmt.Errorf("Failed to find absolute path to \"%s\" (%s)", h.hookPath, err)
	}

	// Create the hook runner code
	var script string
	if runtime.GOOS == "windows" {
		script = "@echo off\n" +
			"SETLOCAL ENABLEDELAYEDEXPANSION\n" +
			"SET > \"" + h.beforeEnvFile.Name() + "\"\n" +
			"CALL \"" + absolutePathToHook + "\"\n" +
			"SET BUILDKITE_LAST_HOOK_EXIT_STATUS=!ERRORLEVEL!\n" +
			"SET > \"" + h.afterEnvFile.Name() + "\"\n" +
			"EXIT %BUILDKITE_LAST_HOOK_EXIT_STATUS%"
	} else {
		script = "#!/bin/bash\n" +
			"export -p > \"" + h.beforeEnvFile.Name() + "\"\n" +
			". \"" + absolutePathToHook + "\"\n" +
			"BUILDKITE_LAST_HOOK_EXIT_STATUS=$?\n" +
			"export -p > \"" + h.afterEnvFile.Name() + "\"\n" +
			"exit $BUILDKITE_LAST_HOOK_EXIT_STATUS"
	}

	// Write the hook script to the runner then close the file so we can run it
	h.scriptFile.WriteString(script)
	h.scriptFile.Close()

	return nil
}

func (h *hookRunner) Path() string {
	return h.scriptFile.Name()
}

func (h *hookRunner) Close() {
	os.Remove(h.scriptFile.Name())
	os.Remove(h.beforeEnvFile.Name())
	os.Remove(h.afterEnvFile.Name())
}

// Environment returns and environment variables exported during the hook run
func (h *hookRunner) Environment() (*env.Environment, error) {
	beforeEnvContents, err := ioutil.ReadFile(h.beforeEnvFile.Name())
	if err != nil {
		return nil, fmt.Errorf("Failed to read \"%s\" (%s)", h.beforeEnvFile.Name(), err)
	}

	afterEnvContents, err := ioutil.ReadFile(h.afterEnvFile.Name())
	if err != nil {
		return nil, fmt.Errorf("Failed to read \"%s\" (%s)", h.afterEnvFile.Name(), err)
	}

	beforeEnv := env.FromExport(string(beforeEnvContents))
	afterEnv := env.FromExport(string(afterEnvContents))

	return afterEnv.Diff(beforeEnv), nil
}

// 	if h.Debug {
// 		h.Shell.Commentf("A hook runner was written to \"%s\" with the following:", tempHookRunnerFile.Name())
// 		h.Shell.Printf("%s", hookScript)
// 	}

// 	h.Shell.Commentf("Executing \"%s\"", path)

// 	// Create a copy of the current env
// 	previousEnviron := h.Shell.Env.Copy()

// 	// Apply our environment
// 	h.Shell.Env = h.Shell.Env.Merge(environ)

// 	// Restore the previous env
// 	defer func() {
// 		h.Shell.Env = previousEnviron
// 	}()

// 	// Run the hook
// 	if err = b.executeScript(h.Shell, tempHookRunnerFile.Name()); err != nil {
// 		return nil, err
// 	}

// 	// Save the hook exit status so other hooks can get access to it
// 	h.Shell.Env.Set("BUILDKITE_LAST_HOOK_EXIT_STATUS", fmt.Sprintf("%d", shell.GetExitCode(hookErr)))

// 	var beforeEnv *env.Environment
// 	var afterEnv *env.Environment

// 	// Compare the ENV current env with the after shots
// 	beforeEnvContents, err := ioutil.ReadFile(tempEnvBeforeFile.Name())
// 	if err != nil {
// 		return nil, fmt.Errorf("Failed to read \"%s\" (%s)", tempEnvBeforeFile.Name(), err)
// 	}
// 	beforeEnv = env.FromExport(string(beforeEnvContents))

// 	afterEnvContents, err := ioutil.ReadFile(tempEnvAfterFile.Name())
// 	if err != nil {
// 		return nil, fmt.Errorf("Failed to read \"%s\" (%s)", tempEnvAfterFile.Name(), err)
// 	}
// 	afterEnv = env.FromExport(string(afterEnvContents))

// 	// Remove the BUILDKITE_LAST_HOOK_EXIT_STATUS from the after
// 	// env (since we don't care about it)
// 	afterEnv.Remove("BUILDKITE_LAST_HOOK_EXIT_STATUS")

// 	return afterEnv.Diff(beforeEnv), hookErr
// }
