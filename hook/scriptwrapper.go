package hook

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"

	"github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/job/shell"
	"github.com/buildkite/agent/v3/shellscript"
	"github.com/buildkite/agent/v3/utils"
)

const (
	hookExitStatusEnv = "BUILDKITE_HOOK_EXIT_STATUS"
	hookWorkingDirEnv = "BUILDKITE_HOOK_WORKING_DIR"

	batchScript = `@echo off
SETLOCAL ENABLEDELAYEDEXPANSION
buildkite-agent env dump > "{{.BeforeEnvFileName}}"
CALL "{{.PathToHook}}"
SET BUILDKITE_HOOK_EXIT_STATUS=!ERRORLEVEL!
SET BUILDKITE_HOOK_WORKING_DIR=%CD%
buildkite-agent env dump > "{{.AfterEnvFileName}}"
EXIT %BUILDKITE_HOOK_EXIT_STATUS%`

	powershellScript = `$ErrorActionPreference = "STOP"
buildkite-agent env dump | Set-Content "{{.BeforeEnvFileName}}"
{{.PathToHook}}
if ($LASTEXITCODE -eq $null) {$Env:BUILDKITE_HOOK_EXIT_STATUS = 0} else {$Env:BUILDKITE_HOOK_EXIT_STATUS = $LASTEXITCODE}
$Env:BUILDKITE_HOOK_WORKING_DIR = $PWD | Select-Object -ExpandProperty Path
buildkite-agent env dump | Set-Content "{{.AfterEnvFileName}}"
exit $Env:BUILDKITE_HOOK_EXIT_STATUS`

	posixShellScript = `{{if .ShebangLine}}{{.ShebangLine}}
{{end -}}
buildkite-agent env dump > "{{.BeforeEnvFileName}}"
. "{{.PathToHook}}"
export BUILDKITE_HOOK_EXIT_STATUS=$?
export BUILDKITE_HOOK_WORKING_DIR="${PWD}"
buildkite-agent env dump > "{{.AfterEnvFileName}}"
exit $BUILDKITE_HOOK_EXIT_STATUS`
)

var (
	batchScriptTmpl      = template.Must(template.New("batch").Parse(batchScript))
	powershellScriptTmpl = template.Must(template.New("pwsh").Parse(powershellScript))
	posixShellScriptTmpl = template.Must(template.New("bash").Parse(posixShellScript))
)

type scriptTemplateInput struct {
	ShebangLine       string
	BeforeEnvFileName string
	AfterEnvFileName  string
	PathToHook        string
}

type HookScriptChanges struct {
	Diff    env.Diff
	afterWd string
}

func (changes *HookScriptChanges) GetAfterWd() (string, error) {
	if changes.afterWd == "" {
		return "", fmt.Errorf("%q was not present in the hook after environment", hookWorkingDirEnv)
	}

	return changes.afterWd, nil
}

type HookExitError struct {
	hookPath string
}

func (e *HookExitError) Error() string {
	return fmt.Sprintf("Hook %q early exited, could not record after environment or working directory", e.hookPath)
}

type scriptWrapperOpt func(*ScriptWrapper)

// Hooks get "sourced" into job execution in the sense that they get the
// environment set for them and then we capture any extra environment variables
// that are exported in the script.

// The tricky thing is that it's impossible to grab the ENV of a child process
// before it finishes, so we've got an awesome (ugly) hack to get around this.
// We write the ENV to file, run the hook and then write the ENV back to another file.
// Then we can use the diff of the two to figure out what changes to make to the
// job executor. Horrible, but effective.

// ScriptWrapper wraps a hook script with env collection and then provides
// a way to get the difference between the environment before the hook is run and
// after it
type ScriptWrapper struct {
	hookPath      string
	os            string
	scriptFile    *os.File
	beforeEnvFile *os.File
	afterEnvFile  *os.File
}

func WithHookPath(path string) scriptWrapperOpt {
	return func(wrap *ScriptWrapper) {
		wrap.hookPath = path
	}
}

func WithOS(os string) scriptWrapperOpt {
	return func(wrap *ScriptWrapper) {
		wrap.os = os
	}
}

// NewScriptWrapper creates and configures a ScriptWrapper.
// Writes temporary files to the filesystem.
func NewScriptWrapper(opts ...scriptWrapperOpt) (*ScriptWrapper, error) {
	wrap := &ScriptWrapper{
		os: runtime.GOOS,
	}

	for _, o := range opts {
		o(wrap)
	}

	if wrap.hookPath == "" {
		return nil, errors.New("hook path was not provided")
	}

	// Extract any shebang line from the hook to copy into the wrapper.
	shebang, err := shellscript.ShebangLine(wrap.hookPath)
	if err != nil {
		return nil, fmt.Errorf("reading hook path: %w", err)
	}

	// Previously we assumed Bash, because the wrapper relied on a Bash-ism.
	// By using `bk-agent env dump`, the wrapper is compatible with /bin/sh.
	//
	// If there is no shebang line, the decision on what shell to use is the
	// responsibility of the job executor.
	//
	// But if the shebang specifies something weird like Ruby 🤪
	// the wrapper won't work. Stick to POSIX shells for now.
	if shebang != "" && !shellscript.IsPOSIXShell(shebang) {
		return nil, fmt.Errorf("hook starts with an unsupported shebang line %q", shebang)
	}

	var isPOSIXHook, isPwshHook bool

	scriptFileName := "buildkite-agent-job-exec-hook-runner"
	isWindows := wrap.os == "windows"

	// we use bash hooks for scripts with no extension, otherwise on windows
	// we probably need a .bat extension
	switch {
	case filepath.Ext(wrap.hookPath) == ".ps1":
		isPwshHook = true
		scriptFileName += ".ps1"

	case filepath.Ext(wrap.hookPath) == "":
		isPOSIXHook = true

	case isWindows:
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
		"buildkite-agent-job-exec-hook-env-before",
	)
	if err != nil {
		return nil, err
	}
	wrap.beforeEnvFile.Close()

	// We'll then pump the ENV _after_ the hook into this temp file
	wrap.afterEnvFile, err = shell.TempFileWithExtension(
		"buildkite-agent-job-exec-hook-env-after",
	)
	if err != nil {
		return nil, err
	}
	wrap.afterEnvFile.Close()

	absolutePathToHook, err := filepath.Abs(wrap.hookPath)
	if err != nil {
		return nil, fmt.Errorf("finding absolute path to %q: %w", wrap.hookPath, err)
	}

	tmplInput := scriptTemplateInput{
		ShebangLine:       shebang,
		BeforeEnvFileName: wrap.beforeEnvFile.Name(),
		AfterEnvFileName:  wrap.afterEnvFile.Name(),
		PathToHook:        absolutePathToHook,
	}

	// Create the hook runner code
	buf := &strings.Builder{}
	switch {
	case isWindows && !isPOSIXHook && !isPwshHook:
		batchScriptTmpl.Execute(buf, tmplInput)

	case isWindows && isPwshHook:
		powershellScriptTmpl.Execute(buf, tmplInput)

	default:
		posixShellScriptTmpl.Execute(buf, tmplInput)
	}
	script := buf.String()

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
func (wrap *ScriptWrapper) Changes() (HookScriptChanges, error) {
	beforeEnvContents, err := os.ReadFile(wrap.beforeEnvFile.Name())
	if err != nil {
		return HookScriptChanges{}, fmt.Errorf("reading file %q: %w", wrap.beforeEnvFile.Name(), err)
	}

	afterEnvContents, err := os.ReadFile(wrap.afterEnvFile.Name())
	if err != nil {
		return HookScriptChanges{}, fmt.Errorf("reading file %q: %w", wrap.afterEnvFile.Name(), err)
	}

	// An empty afterEnvFile indicates that the hook early-exited from within the
	// ScriptWrapper, so the working directory and environment changes weren't
	// captured.
	if len(afterEnvContents) == 0 {
		return HookScriptChanges{}, &HookExitError{hookPath: wrap.hookPath}
	}

	var (
		beforeEnv *env.Environment
		afterEnv  *env.Environment
	)

	err = json.Unmarshal(beforeEnvContents, &beforeEnv)
	if err != nil {
		return HookScriptChanges{}, fmt.Errorf("failed to unmarshal before env file: %w, file contents: %s", err, string(beforeEnvContents))
	}

	err = json.Unmarshal(afterEnvContents, &afterEnv)
	if err != nil {
		return HookScriptChanges{}, fmt.Errorf("failed to unmarshal after env file: %w, file contents: %s", err, string(afterEnvContents))
	}

	diff := afterEnv.Diff(beforeEnv)

	// Pluck the after wd from the diff before removing the key from the diff
	afterWd := diff.Added[hookWorkingDirEnv]
	if afterWd == "" {
		if change, ok := diff.Changed[hookWorkingDirEnv]; ok {
			afterWd = change.New
		}
	}

	diff.Remove(hookExitStatusEnv)
	diff.Remove(hookWorkingDirEnv)

	// Bash sets this, but we don't care about it
	diff.Remove("_")

	return HookScriptChanges{Diff: diff, afterWd: afterWd}, nil
}
