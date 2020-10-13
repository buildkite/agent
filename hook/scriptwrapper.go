package hook

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"

	"github.com/buildkite/agent/v3/bootstrap/shell"
	"github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/utils"
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

// ScriptWrapper wraps a hook script with env collection and then provides
// a way to get the difference between the environment before the hook is run and
// after it
type ScriptWrapper struct {
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

// CreateScriptWrapper creates and configures a ScriptWrapper.
// Writes temporary files to the filesystem.
func CreateScriptWrapper(hookPath string) (*ScriptWrapper, error) {
	var wrap = &ScriptWrapper{
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
	wrap.scriptFile, err = shell.TempFileWithExtension(scriptFileName)
	if err != nil {
		return nil, err
	}
	defer wrap.scriptFile.Close()

	// We'll pump the ENV before the hook into this temp file
	wrap.beforeEnvFile, err = shell.TempFileWithExtension(
		`buildkite-agent-bootstrap-hook-env-before`,
	)
	if err != nil {
		return nil, err
	}
	wrap.beforeEnvFile.Close()

	// We'll then pump the ENV _after_ the hook into this temp file
	wrap.afterEnvFile, err = shell.TempFileWithExtension(
		`buildkite-agent-bootstrap-hook-env-after`,
	)
	if err != nil {
		return nil, err
	}
	wrap.afterEnvFile.Close()

	absolutePathToHook, err := filepath.Abs(wrap.hookPath)
	if err != nil {
		return nil, fmt.Errorf("Failed to find absolute path to \"%s\" (%s)", wrap.hookPath, err)
	}

	wrap.beforeWd, err = os.Getwd()
	if err != nil {
		return nil, err
	}

	// Create the hook runner code
	var script string
	if isWindows && !isBashHook && !isPwshHook {
		script = "@echo off\n" +
			"SETLOCAL ENABLEDELAYEDEXPANSION\n" +
			"SET > \"" + wrap.beforeEnvFile.Name() + "\"\n" +
			"CALL \"" + absolutePathToHook + "\"\n" +
			"SET " + hookExitStatusEnv + "=!ERRORLEVEL!\n" +
			"SET " + hookWorkingDirEnv + "=%CD%\n" +
			"SET > \"" + wrap.afterEnvFile.Name() + "\"\n" +
			"EXIT %" + hookExitStatusEnv + "%"
	} else if isWindows && isPwshHook {
		script = `$ErrorActionPreference = "STOP"\n` +
			`Get-ChildItem Env: | Foreach-Object {$($_.Name)=$($_.Value)"} | Set-Content "` + wrap.beforeEnvFile.Name() + `\n` +
			absolutePathToHook + `\n` +
			`if ($LASTEXITCODE -eq $null) {$Env:` + hookExitStatusEnv + ` = 0} else {$Env:` + hookExitStatusEnv + ` = $LASTEXITCODE}\n` +
			`$Env:` + hookWorkingDirEnv + ` = $PWD | Select-Object -ExpandProperty Path\n` +
			`Get-ChildItem Env: | Foreach-Object {"$($_.Name)=$($_.Value)"} | Set-Content "` + wrap.afterEnvFile.Name() + `"\n` +
			`exit $Env:` + hookExitStatusEnv
	} else {
		script = "export -p > \"" + filepath.ToSlash(wrap.beforeEnvFile.Name()) + "\"\n" +
			". \"" + filepath.ToSlash(absolutePathToHook) + "\"\n" +
			"export " + hookExitStatusEnv + "=$?\n" +
			"export " + hookWorkingDirEnv + "=$PWD\n" +
			"export -p > \"" + filepath.ToSlash(wrap.afterEnvFile.Name()) + "\"\n" +
			"exit $" + hookExitStatusEnv
	}

	// Write the hook script to the runner then close the file so we can run it
	_, err = wrap.scriptFile.WriteString(script)
	if err != nil {
		return nil, err
	}

	// Make script executable
	err = utils.ChmodExecutable(wrap.scriptFile.Name())
	if err != nil {
		return wrap, err
	}

	return wrap, nil
}

// Path returns the path to the wrapper script, this is the one that should be executed
func (wrap *ScriptWrapper) Path() string {
	return wrap.scriptFile.Name()
}

// Close cleans up the wrapper script and the environment files
func (wrap *ScriptWrapper) Close() {
	os.Remove(wrap.scriptFile.Name())
	os.Remove(wrap.beforeEnvFile.Name())
	os.Remove(wrap.afterEnvFile.Name())
}

// Changes returns the changes in the environment and working dir after the hook script runs
func (wrap *ScriptWrapper) Changes() (hookScriptChanges, error) {
	beforeEnvContents, err := ioutil.ReadFile(wrap.beforeEnvFile.Name())
	if err != nil {
		return hookScriptChanges{}, fmt.Errorf("Failed to read \"%s\" (%s)", wrap.beforeEnvFile.Name(), err)
	}

	afterEnvContents, err := ioutil.ReadFile(wrap.afterEnvFile.Name())
	if err != nil {
		return hookScriptChanges{}, fmt.Errorf("Failed to read \"%s\" (%s)", wrap.afterEnvFile.Name(), err)
	}

	beforeEnv := env.FromExport(string(beforeEnvContents))
	afterEnv := env.FromExport(string(afterEnvContents))
	diff := afterEnv.Diff(beforeEnv)
	wd, _ := diff.Get(hookWorkingDirEnv)

	diff.Remove(hookExitStatusEnv)
	diff.Remove(hookWorkingDirEnv)

	return hookScriptChanges{Env: diff, Dir: wd}, nil
}
