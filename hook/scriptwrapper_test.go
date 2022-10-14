package hook

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/bootstrap/shell"
	"github.com/buildkite/agent/v3/env"
	"github.com/buildkite/bintest/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunningHookDetectsChangedEnvironment(t *testing.T) {
	ctx := context.Background()
	var script []string

	if runtime.GOOS != "windows" {
		script = []string{
			"#!/bin/bash",
			"export LLAMAS=rock",
			"export Alpacas=\"are ok\"",
			"echo hello world",
		}
	} else {
		script = []string{
			"@echo off",
			"set LLAMAS=rock",
			"set Alpacas=are ok",
			"echo hello world",
		}
	}

	agent, cleanup, err := mockAgent()
	require.NoError(t, err)

	defer cleanup()

	wrapper := newTestScriptWrapper(t, script)
	defer os.Remove(wrapper.Path())

	sh := shell.NewTestShell(t)

	if err := sh.RunScript(ctx, wrapper.Path(), nil); err != nil {
		t.Fatal(err)
	}

	changes, err := wrapper.Changes()
	if err != nil {
		t.Fatal(err)
	}

	// Windows’ batch 'SET >' normalises environment variables case so we apply
	// the 'expected' and 'actual' diffs to a blank Environment which handles
	// case normalisation for us
	expected := (&env.Environment{}).Apply(env.Diff{
		Added: map[string]string{
			"LLAMAS":  "rock",
			"Alpacas": "are ok",
		},
		Changed: map[string]env.DiffPair{},
		Removed: map[string]struct{}{},
	})

	actual := (&env.Environment{}).Apply(changes.Diff)

	// The strict equals check here also ensures we aren't bubbling up the
	// internal BUILDKITE_HOOK_EXIT_STATUS and BUILDKITE_HOOK_WORKING_DIR
	// environment variables
	assert.Equal(t, expected, actual)

	err = agent.CheckAndClose(t)
	require.NoError(t, err)
}

func TestHookScriptsAreGeneratedCorrectlyOnWindowsBatch(t *testing.T) {
	t.Parallel()

	hookFile, err := shell.TempFileWithExtension("hookName.bat")
	assert.NoError(t, err)

	_, err = fmt.Fprintln(hookFile, `echo Hello There!`)
	assert.NoError(t, err)

	hookFile.Close()

	wrapper, err := NewScriptWrapper(
		WithHookPath(hookFile.Name()),
		WithOS("windows"),
	)
	assert.NoError(t, err)

	defer wrapper.Close()

	// The double percent signs %% are sprintf-escaped literal percent signs. Escaping hell is impossible to get out of.
	// See: https://pkg.go.dev/fmt > ctrl-f for "%%"
	scriptTemplate := `@echo off
SETLOCAL ENABLEDELAYEDEXPANSION
buildkite-agent env > "%s"
CALL "%s"
SET BUILDKITE_HOOK_EXIT_STATUS=!ERRORLEVEL!
SET BUILDKITE_HOOK_WORKING_DIR=%%CD%%
buildkite-agent env > "%s"
EXIT %%BUILDKITE_HOOK_EXIT_STATUS%%`

	assertScriptLike(t, scriptTemplate, hookFile.Name(), wrapper)
}

func TestHookScriptsAreGeneratedCorrectlyOnWindowsPowershell(t *testing.T) {
	t.Parallel()

	hookFile, err := shell.TempFileWithExtension("hookName.ps1")
	assert.NoError(t, err)

	_, err = fmt.Fprintln(hookFile, `Write-Output "Hello There!"`)
	assert.NoError(t, err)

	hookFile.Close()

	wrapper, err := NewScriptWrapper(
		WithHookPath(hookFile.Name()),
		WithOS("windows"),
	)
	assert.NoError(t, err)

	defer wrapper.Close()

	scriptTemplate := `$ErrorActionPreference = "STOP"
buildkite-agent env | Set-Content "%s"
%s
if ($LASTEXITCODE -eq $null) {$Env:BUILDKITE_HOOK_EXIT_STATUS = 0} else {$Env:BUILDKITE_HOOK_EXIT_STATUS = $LASTEXITCODE}
$Env:BUILDKITE_HOOK_WORKING_DIR = $PWD | Select-Object -ExpandProperty Path
buildkite-agent env | Set-Content "%s"
exit $Env:BUILDKITE_HOOK_EXIT_STATUS`

	assertScriptLike(t, scriptTemplate, hookFile.Name(), wrapper)
}

func TestHookScriptsAreGeneratedCorrectlyOnUnix(t *testing.T) {
	t.Parallel()

	hookFile, err := shell.TempFileWithExtension("hookName")
	assert.NoError(t, err)

	_, err = fmt.Fprintln(hookFile, `echo "Hello There!"`)
	assert.NoError(t, err)

	hookFile.Close()

	wrapper, err := NewScriptWrapper(
		WithHookPath(hookFile.Name()),
		WithOS("linux"),
	)
	assert.NoError(t, err)

	defer wrapper.Close()

	scriptTemplate := `buildkite-agent env > "%s"
. "%s"
export BUILDKITE_HOOK_EXIT_STATUS=$?
export BUILDKITE_HOOK_WORKING_DIR=$PWD
buildkite-agent env > "%s"
exit $BUILDKITE_HOOK_EXIT_STATUS`

	assertScriptLike(t, scriptTemplate, hookFile.Name(), wrapper)
}

func TestRunningHookDetectsChangedWorkingDirectory(t *testing.T) {
	agent, cleanup, err := mockAgent()
	require.NoError(t, err)

	defer cleanup()

	tempDir, err := ioutil.TempDir("", "hookwrapperdir")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	ctx := context.Background()
	var script []string

	if runtime.GOOS != "windows" {
		script = []string{
			"#!/bin/bash",
			"mkdir mysubdir",
			"cd mysubdir",
			"echo hello world",
		}
	} else {
		script = []string{
			"@echo off",
			"mkdir mysubdir",
			"cd mysubdir",
			"echo hello world",
		}
	}

	wrapper := newTestScriptWrapper(t, script)
	defer os.Remove(wrapper.Path())

	sh := shell.NewTestShell(t)
	if err := sh.Chdir(tempDir); err != nil {
		t.Fatal(err)
	}

	if err := sh.RunScript(ctx, wrapper.Path(), nil); err != nil {
		t.Fatal(err)
	}

	changes, err := wrapper.Changes()
	if err != nil {
		t.Fatal(err)
	}

	expected, err := filepath.EvalSymlinks(filepath.Join(tempDir, "mysubdir"))
	if err != nil {
		t.Fatal(err)
	}

	afterWd, err := changes.GetAfterWd()
	if err != nil {
		t.Fatal(err)
	}

	changesDir, err := filepath.EvalSymlinks(afterWd)
	if err != nil {
		t.Fatal(err)
	}

	if changesDir != expected {
		t.Fatalf("Expected working dir of %q, got %q", expected, changesDir)
	}

	err = agent.CheckAndClose(t)
	require.NoError(t, err)
}

func newTestScriptWrapper(t *testing.T, script []string) *ScriptWrapper {
	hookName := "hookwrapper"
	if runtime.GOOS == "windows" {
		hookName += ".bat"
	}

	hookFile, err := shell.TempFileWithExtension(hookName)
	assert.NoError(t, err)

	for _, line := range script {
		_, err = fmt.Fprintln(hookFile, line)
		assert.NoError(t, err)
	}

	hookFile.Close()

	wrapper, err := NewScriptWrapper(WithHookPath(hookFile.Name()))
	assert.NoError(t, err)

	return wrapper
}

func assertScriptLike(t *testing.T, scriptTemplate, hookFileName string, wrapper *ScriptWrapper) {
	file, err := os.Open(wrapper.scriptFile.Name())
	assert.NoError(t, err)

	defer file.Close()

	wrapperScriptContents, err := ioutil.ReadAll(file)
	assert.NoError(t, err)

	expected := fmt.Sprintf(scriptTemplate, wrapper.beforeEnvFile.Name(), hookFileName, wrapper.afterEnvFile.Name())

	assert.Equal(t, expected, string(wrapperScriptContents))
}

func mockAgent() (*bintest.Mock, func(), error) {
	tmpPathDir, err := os.MkdirTemp("", "scriptwrapper-path")
	if err != nil {
		return nil, func() {}, err
	}

	oldPath := os.Getenv("PATH")
	os.Setenv("PATH", tmpPathDir+string(os.PathListSeparator)+oldPath)

	cleanup := func() {
		err := os.Setenv("PATH", oldPath)
		if err != nil {
			panic(err)
		}

		err = os.RemoveAll(tmpPathDir)
		if err != nil {
			panic(err)
		}
	}

	agent, err := bintest.NewMock(filepath.Join(tmpPathDir, "buildkite-agent"))
	if err != nil {
		return nil, func() {}, err
	}

	agent.Expect("env").
		Exactly(2).
		AndCallFunc(func(c *bintest.Call) {
			envMap := map[string]string{}

			for _, e := range c.Env { // The env from the call
				k, v, _ := strings.Cut(e, "=")
				envMap[k] = v
			}

			envJson, err := json.Marshal(envMap)
			if err != nil {
				fmt.Println("Failed to marshal env map in mocked agent call:", err)
				c.Exit(1)
			}

			c.Stdout.Write(envJson)
			c.Exit(0)
		})

	return agent, cleanup, nil
}
