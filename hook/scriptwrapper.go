package hook

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"text/template"

	"github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/internal/job/shell"
	"github.com/buildkite/agent/v3/internal/shellscript"
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
	os            string
	wrapperPath   string
	beforeEnvPath string
	afterEnvPath  string
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
	// the wrapper won't work. We do support ruby (and other interpreted) hooks via polyglot hooks (see: https://github.com/buildkite/agent/pull/2040),
	// but they should never be wrapped, and if they have been, something has gone wrong.
	if shebang != "" && !shellscript.IsPOSIXShell(shebang) {
		return nil, fmt.Errorf("scriptwrapper tried to wrap hook with invalid shebang: %q", shebang)
	}

	var isPOSIXHook, isPwshHook bool

	scriptFileName := "buildkite-agent-bootstrap-hook-runner"
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

	var tmpl *template.Template
	switch {
	case isWindows && !isPOSIXHook && !isPwshHook:
		tmpl = batchScriptTmpl
	case isWindows && isPwshHook:
		tmpl = powershellScriptTmpl
	default:
		tmpl = posixShellScriptTmpl
	}

	// Create a temporary file that we'll put the hook runner code in
	scriptFile, err := shell.TempFileWithExtension(scriptFileName)
	if err != nil {
		return nil, err
	}
	if err := scriptFile.Close(); err != nil {
		return nil, fmt.Errorf("failed to close script file: %w", err)
	}
	wrap.wrapperPath = scriptFile.Name()

	// We'll pump the ENV before the hook into this temp file
	beforeEnvFile, err := shell.TempFileWithExtension("buildkite-agent-bootstrap-hook-env-before")
	if err != nil {
		return nil, err
	}
	if err := beforeEnvFile.Close(); err != nil {
		return nil, fmt.Errorf("failed to close before env file: %w", err)
	}
	wrap.beforeEnvPath = beforeEnvFile.Name()

	// We'll then pump the ENV _after_ the hook into this temp file
	afterEnvFile, err := shell.TempFileWithExtension("buildkite-agent-bootstrap-hook-env-after")
	if err != nil {
		return nil, err
	}
	if err := afterEnvFile.Close(); err != nil {
		return nil, fmt.Errorf("failed to close after env file: %w", err)
	}
	wrap.afterEnvPath = afterEnvFile.Name()

	absolutePathToHook, err := filepath.Abs(wrap.hookPath)
	if err != nil {
		return nil, fmt.Errorf("finding absolute path to %q: %w", wrap.hookPath, err)
	}

	if err := WriteScriptWrapper(
		tmpl,
		scriptTemplateInput{
			ShebangLine:       shebang,
			BeforeEnvFileName: wrap.beforeEnvPath,
			AfterEnvFileName:  wrap.afterEnvPath,
			PathToHook:        absolutePathToHook,
		},
		scriptFileName,
	); err != nil {
		return nil, err
	}

	return wrap, nil
}

func WriteScriptWrapper(
	tmpl *template.Template,
	input scriptTemplateInput,
	scriptWrapperPath string,
) error {
	f, err := os.OpenFile(scriptWrapperPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o700)
	if err != nil {
		return err
	}
	defer f.Close()
	return tmpl.Execute(f, input)
}

// Path returns the path to the wrapper script, this is the one that should be executed
func (wrap *ScriptWrapper) Path() string {
	return wrap.wrapperPath
}

// Close cleans up the wrapper script and the environment files
func (wrap *ScriptWrapper) Close() {
	_ = os.Remove(wrap.wrapperPath)
	_ = os.Remove(wrap.beforeEnvPath)
	_ = os.Remove(wrap.afterEnvPath)
}

// Changes returns the changes in the environment and working dir after the hook script runs
func (wrap *ScriptWrapper) Changes() (HookScriptChanges, error) {
	beforeEnvContents, err := os.ReadFile(wrap.beforeEnvPath)
	if err != nil {
		return HookScriptChanges{}, fmt.Errorf("reading file %q: %w", wrap.beforeEnvPath, err)
	}

	afterEnvContents, err := os.ReadFile(wrap.afterEnvPath)
	if err != nil {
		return HookScriptChanges{}, fmt.Errorf("reading file %q: %w", wrap.afterEnvPath, err)
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
