package bootstrap

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"

	"github.com/buildkite/agent/v3/bootstrap/shell"
	"github.com/buildkite/agent/v3/env"
)

const (
	hookExitStatusEnv = `BUILDKITE_HOOK_EXIT_STATUS`
	hookWorkingDirEnv = `BUILDKITE_HOOK_WORKING_DIR`
)

// Hooks get "sourced" into the bootstrap in the sense that they get the
// environment set for them and then we capture any extra environment variables
// that are exported in the script.

// The tricky thing is that it's impossible to grab the ENV of a child process
// before it finishes, so we've got an awesome (ugly) hack to get around this.
// We write the ENV to file, run the hook and then write the ENV back to another file.
// Then we can use the diff of the two to figure out what changes to make to the
// bootstrap. Horrible, but effective.

// hookScriptWrapper wraps a hook script with env collection and then provides
// a way to get the difference between the environment before the hook is run and
// after it
type hookScriptWrapper struct {
	hookPath      string
	scriptFile    *os.File
	beforeEnvFile *os.File
	afterEnvFile  *os.File
	beforeWd      string
}

type hookScriptChanges struct {
	Env *env.Environment
	Dir string
}

func newHookScriptWrapper(hookPath string) (*hookScriptWrapper, error) {
	var h = &hookScriptWrapper{
		hookPath: hookPath,
	}

	var err error
	var scriptFileName string = `buildkite-agent-bootstrap-hook-runner`
	var isBashHook bool
	var isPwshHook bool
	var isWindows = runtime.GOOS == "windows"

	// we use bash hooks for scripts with no extension, otherwise on windows
	// we probably need a .bat extension
	if filepath.Ext(hookPath) == ".ps1" {
		isPwshHook = true
		scriptFileName += ".ps1"
	} else if filepath.Ext(hookPath) == "" {
		isBashHook = true
	} else if isWindows {
		scriptFileName += ".bat"
	}

	// Create a temporary file that we'll put the hook runner code in
	h.scriptFile, err = shell.TempFileWithExtension(scriptFileName)
	if err != nil {
		return nil, err
	}
	defer h.scriptFile.Close()

	// We'll pump the ENV before the hook into this temp file
	h.beforeEnvFile, err = shell.TempFileWithExtension(
		`buildkite-agent-bootstrap-hook-env-before`,
	)
	if err != nil {
		return nil, err
	}
	h.beforeEnvFile.Close()

	// We'll then pump the ENV _after_ the hook into this temp file
	h.afterEnvFile, err = shell.TempFileWithExtension(
		`buildkite-agent-bootstrap-hook-env-after`,
	)
	if err != nil {
		return nil, err
	}
	h.afterEnvFile.Close()

	absolutePathToHook, err := filepath.Abs(h.hookPath)
	if err != nil {
		return nil, fmt.Errorf("Failed to find absolute path to \"%s\" (%s)", h.hookPath, err)
	}

	h.beforeWd, err = os.Getwd()
	if err != nil {
		return nil, err
	}

	// Create the hook runner code
	var script string
	if isWindows && !isBashHook && !isPwshHook {
		script = "@echo off\n" +
			"SETLOCAL ENABLEDELAYEDEXPANSION\n" +
			"SET > \"" + h.beforeEnvFile.Name() + "\"\n" +
			"CALL \"" + absolutePathToHook + "\"\n" +
			"SET " + hookExitStatusEnv + "=!ERRORLEVEL!\n" +
			"SET " + hookWorkingDirEnv + "=%CD%\n" +
			"SET > \"" + h.afterEnvFile.Name() + "\"\n" +
			"EXIT %" + hookExitStatusEnv + "%"
	} else if isWindows && isPwshHook {
		script = "$ErrorActionPreference = \"STOP\"" + "\n" +
			"Get-ChildItem Env: | Foreach-Object {$($_.Name)=$($_.Value)\"} | Set-Content \" " + h.beforeEnvFile.Name() + "\n" +
			absolutePathToHook + "\n" +
			"if ($LASTEXITCODE -eq $null) {$Env:" + hookExitStatusEnv + " = 0} else {$Env:" + hookExitStatusEnv + " = $LASTEXITCODE}\n" +
			"$Env:" + hookWorkingDirEnv + " = $PWD | Select-Object -ExpandProperty Path\n" +
			"Get-ChildItem Env: | Foreach-Object {\"$($_.Name)=$($_.Value)\"} | Set-Content " + h.afterEnvFile.Name() + "\"\n" +
			"exit $Env:" + hookExitStatusEnv
	} else {
		script = "export -p > \"" + filepath.ToSlash(h.beforeEnvFile.Name()) + "\"\n" +
			". \"" + filepath.ToSlash(absolutePathToHook) + "\"\n" +
			"export " + hookExitStatusEnv + "=$?\n" +
			"export " + hookWorkingDirEnv + "=$PWD\n" +
			"export -p > \"" + filepath.ToSlash(h.afterEnvFile.Name()) + "\"\n" +
			"exit $" + hookExitStatusEnv
	}

	// Write the hook script to the runner then close the file so we can run it
	_, err = h.scriptFile.WriteString(script)
	if err != nil {
		return nil, err
	}

	// Make script executable
	err = addExecutePermissionToFile(h.scriptFile.Name())
	if err != nil {
		return h, err
	}

	return h, nil
}

// Path returns the path to the wrapper script, this is the one that should be executed
func (h *hookScriptWrapper) Path() string {
	return h.scriptFile.Name()
}

// Close cleans up the wrapper script and the environment files
func (h *hookScriptWrapper) Close() {
	os.Remove(h.scriptFile.Name())
	os.Remove(h.beforeEnvFile.Name())
	os.Remove(h.afterEnvFile.Name())
}

// Changes returns the changes in the environment and working dir after the hook script runs
func (h *hookScriptWrapper) Changes() (hookScriptChanges, error) {
	beforeEnvContents, err := ioutil.ReadFile(h.beforeEnvFile.Name())
	if err != nil {
		return hookScriptChanges{}, fmt.Errorf("Failed to read \"%s\" (%s)", h.beforeEnvFile.Name(), err)
	}

	afterEnvContents, err := ioutil.ReadFile(h.afterEnvFile.Name())
	if err != nil {
		return hookScriptChanges{}, fmt.Errorf("Failed to read \"%s\" (%s)", h.afterEnvFile.Name(), err)
	}

	beforeEnv := env.FromExport(string(beforeEnvContents))
	afterEnv := env.FromExport(string(afterEnvContents))
	diff := afterEnv.Diff(beforeEnv)
	wd, _ := diff.Get(hookWorkingDirEnv)

	diff.Remove(hookExitStatusEnv)
	diff.Remove(hookWorkingDirEnv)

	return hookScriptChanges{Env: diff, Dir: wd}, nil
}
