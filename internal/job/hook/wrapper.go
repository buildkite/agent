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
	"github.com/buildkite/agent/v3/internal/shellscript"
	"github.com/buildkite/agent/v3/internal/tempfile"
)

type TemplateType int

const (
	// BatchTemplateType indicates to WriteHookWrapper to write a batch file
	// for the hook wrapper
	BatchTemplateType TemplateType = iota

	// PowershellTemplateType indicates to WriteHookWrapper to write a Powershell
	// file for the hook wrapper
	PowershellTemplateType

	// PosixShellTemplateType indicates to WriteHookWrapper to write a POSIX shell
	// script for the hook wrapper
	PosixShellTemplateType
)

const (
	hookExitStatusEnv = "BUILDKITE_HOOK_EXIT_STATUS"
	hookWorkingDirEnv = "BUILDKITE_HOOK_WORKING_DIR"
	hookWrapperDir    = "buildkite-agent-hook-wrapper"

	batchWrapper = `@echo off
SETLOCAL ENABLEDELAYEDEXPANSION
{{.AgentBinary}} env dump > "{{.BeforeEnvFileName}}"
CALL "{{.PathToHook}}"
SET BUILDKITE_HOOK_EXIT_STATUS=!ERRORLEVEL!
SET BUILDKITE_HOOK_WORKING_DIR=%CD%
{{.AgentBinary}} env dump > "{{.AfterEnvFileName}}"
EXIT %BUILDKITE_HOOK_EXIT_STATUS%`

	powershellWrapper = `$ErrorActionPreference = "STOP"
{{.AgentBinary}} env dump | Set-Content "{{.BeforeEnvFileName}}"
. {{.PathToHook}}
if ($LASTEXITCODE -eq $null) {
  $Env:BUILDKITE_HOOK_EXIT_STATUS = 0
} else {
  $Env:BUILDKITE_HOOK_EXIT_STATUS = $LASTEXITCODE
}
$Env:BUILDKITE_HOOK_WORKING_DIR = $PWD | Select-Object -ExpandProperty Path
{{.AgentBinary}} env dump | Set-Content "{{.AfterEnvFileName}}"
exit $Env:BUILDKITE_HOOK_EXIT_STATUS`

	posixShellWrapper = `{{if .ShebangLine}}{{.ShebangLine}}
{{end -}}
"{{.AgentBinary}}" env dump > "{{.BeforeEnvFileName}}"
. "{{.PathToHook}}"
export BUILDKITE_HOOK_EXIT_STATUS=$?
export BUILDKITE_HOOK_WORKING_DIR="${PWD}"
"{{.AgentBinary}}" env dump > "{{.AfterEnvFileName}}"
exit $BUILDKITE_HOOK_EXIT_STATUS`
)

var (
	batchWrapperTmpl      = template.Must(template.New("batch").Parse(batchWrapper))
	powershellWrapperTmpl = template.Must(template.New("pwsh").Parse(powershellWrapper))
	posixShellWrapperTmpl = template.Must(template.New("bash").Parse(posixShellWrapper))

	ErrNoHookPath = errors.New("hook path was not provided")
)

type WrapperTemplateInput struct {
	AgentBinary       string
	ShebangLine       string
	BeforeEnvFileName string
	AfterEnvFileName  string
	PathToHook        string
}

type EnvChanges struct {
	Diff    env.Diff
	afterWd string
}

func (changes *EnvChanges) GetAfterWd() (string, error) {
	if changes.afterWd == "" {
		return "", fmt.Errorf("%q was not present in the hook after environment", hookWorkingDirEnv)
	}

	return changes.afterWd, nil
}

type ExitError struct {
	hookPath string
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("hook %q early exited", e.hookPath)
}

type WrapperOpt func(*Wrapper)

// Hooks get "sourced" into the bootstrap in the sense that they get the
// environment set for them and then we capture any extra environment variables
// that are exported in the script.

// The tricky thing is that it's impossible to grab the ENV of a child process
// before it finishes, so we've got an awesome (ugly) hack to get around this.
// We write the ENV to file, run the hook and then write the ENV back to another file.
// Then we can use the diff of the two to figure out what changes to make to the
// bootstrap. Horrible, but effective.

// Wrapper wraps a hook script with env collection and then provides
// a way to get the difference between the environment before the hook is run and
// after it
type Wrapper struct {
	hookPath      string
	os            string
	tempDir       string
	wrapperPath   string
	beforeEnvPath string
	afterEnvPath  string
}

func WithPath(path string) WrapperOpt {
	return func(wrap *Wrapper) {
		wrap.hookPath = path
	}
}

func WithOS(o string) WrapperOpt {
	return func(wrap *Wrapper) {
		wrap.os = o
	}
}

// NewWrapper creates and configures a hook.Wrapper.
// Writes temporary files to the filesystem.
func NewWrapper(opts ...WrapperOpt) (*Wrapper, error) {
	wrap := &Wrapper{
		os: runtime.GOOS,
	}

	for _, o := range opts {
		o(wrap)
	}

	if wrap.hookPath == "" {
		return nil, ErrNoHookPath
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
	// the wrapper won't work. We do support ruby (and other interpreted) hooks via polyglot hooks
	// (see: https://github.com/buildkite/agent/pull/2040),
	// but they should never be wrapped, and if they have been, something has gone wrong.
	if shebang != "" && !shellscript.IsPOSIXShell(shebang) {
		return nil, fmt.Errorf("scriptwrapper tried to wrap hook with invalid shebang: %q", shebang)
	}

	var isPOSIXHook, isPwshHook bool

	scriptFileName := "hook-script-wrapper"
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

	var templateType TemplateType
	switch {
	case isWindows && !isPOSIXHook && !isPwshHook:
		templateType = BatchTemplateType
	case isWindows && isPwshHook:
		templateType = PowershellTemplateType
	default:
		templateType = PosixShellTemplateType
	}

	// The os.TempDir might not exist, since user can set $TMPDIR.
	// Although we attempt to do the same in job_runner, part of job_runner runs before backend env
	// populated the process.
	// So TLDR, $TMPDIR could change between job_runner and hook wrapper.
	osTempDir := os.TempDir()
	if _, err := os.Stat(osTempDir); os.IsNotExist(err) {
		if err = os.MkdirAll(osTempDir, 0o777); err != nil {
			return nil, err
		}
	}

	// On systems where multiple buildkite-agents are running under different
	// users, a shared path could be owned by a different user.
	// Creating a new temporary directory to hold the temporary files avoids
	// this issue and makes cleanup easier.
	tempDir, err := os.MkdirTemp(osTempDir, hookWrapperDir)
	if err != nil {
		return nil, fmt.Errorf("creating temporary directory for hook wrapper: %w", err)
	}
	wrap.tempDir = tempDir

	beforeEnvPath, err := tempfile.NewClosed(
		tempfile.WithDir(tempDir),
		tempfile.WithName("hook-before-env"),
	)
	if err != nil {
		return nil, err
	}
	wrap.beforeEnvPath = beforeEnvPath

	afterEnvPath, err := tempfile.NewClosed(
		tempfile.WithDir(tempDir),
		tempfile.WithName("hook-after-env"),
	)
	if err != nil {
		return nil, err
	}
	wrap.afterEnvPath = afterEnvPath

	absolutePathToHook, err := filepath.Abs(wrap.hookPath)
	if err != nil {
		return nil, fmt.Errorf("finding absolute path to %q: %w", wrap.hookPath, err)
	}

	buildkiteAgent, err := os.Executable()
	if err != nil {
		return nil, err
	}

	templateInput := WrapperTemplateInput{
		AgentBinary:       buildkiteAgent,
		ShebangLine:       shebang,
		BeforeEnvFileName: wrap.beforeEnvPath,
		AfterEnvFileName:  wrap.afterEnvPath,
		PathToHook:        absolutePathToHook,
	}

	wrapperPath, err := WriteHookWrapper(
		templateType,
		templateInput,
		tempDir,
		scriptFileName,
	)
	if err != nil {
		return nil, err
	}
	wrap.wrapperPath = wrapperPath

	return wrap, nil
}

// WriteHookWrapper will write a hook wrapper script to a temporary file with the same extension as,
// `hookWrapperName`. It will return the name of the temporary file. The file will be executable.
// It will be created from the template specified by `templateType` with data from `input`.
func WriteHookWrapper(
	templateType TemplateType,
	input WrapperTemplateInput,
	dir string,
	hookWrapperName string,
) (string, error) {
	f, err := tempfile.New(
		tempfile.WithDir(dir),
		tempfile.WithName(hookWrapperName),
		tempfile.KeepingExtension(),
		tempfile.WithPerms(0o700),
	)
	if err != nil {
		return "", err
	}
	defer f.Close() //nolint:errcheck // Close is checked below.

	var tmpl *template.Template
	switch templateType {
	case BatchTemplateType:
		tmpl = batchWrapperTmpl
	case PowershellTemplateType:
		tmpl = powershellWrapperTmpl
	case PosixShellTemplateType:
		tmpl = posixShellWrapperTmpl
	}

	if err := tmpl.Execute(f, input); err != nil {
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return f.Name(), nil
}

// Path returns the path to the wrapper script, this is the one that should be executed
func (w *Wrapper) Path() string {
	return w.wrapperPath
}

// Close cleans up the wrapper script and the environment files. Ignores errors, in
// particular the error from os.Remove if the file doesn't exist.
func (w *Wrapper) Close() {
	_ = os.RemoveAll(w.tempDir)
}

// Changes returns the changes in the environment and working dir after the hook script runs
func (w *Wrapper) Changes() (EnvChanges, error) {
	beforeEnvContents, err := os.ReadFile(w.beforeEnvPath)
	if err != nil {
		return EnvChanges{}, fmt.Errorf("reading file %q: %w", w.beforeEnvPath, err)
	}

	afterEnvContents, err := os.ReadFile(w.afterEnvPath)
	if err != nil {
		return EnvChanges{}, fmt.Errorf("reading file %q: %w", w.afterEnvPath, err)
	}

	// An empty afterEnvFile indicates that the hook early-exited from within the
	// ScriptWrapper, so the working directory and environment changes weren't
	// captured.
	if len(afterEnvContents) == 0 {
		return EnvChanges{}, &ExitError{hookPath: w.hookPath}
	}

	var (
		beforeEnv *env.Environment
		afterEnv  *env.Environment
	)

	if err := json.Unmarshal(beforeEnvContents, &beforeEnv); err != nil {
		return EnvChanges{}, fmt.Errorf(
			"failed to unmarshal before env file: %w, file contents: %s",
			err,
			beforeEnvContents,
		)
	}

	if err := json.Unmarshal(afterEnvContents, &afterEnv); err != nil {
		return EnvChanges{}, fmt.Errorf(
			"failed to unmarshal after env file: %w, file contents: %s",
			err,
			afterEnvContents,
		)
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

	return EnvChanges{Diff: diff, afterWd: afterWd}, nil
}
