package bootstrap

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/buildkite/agent/bootstrap/shell"
	"github.com/buildkite/agent/env"
)

func TestRunningHookDetectsChangedEnvironment(t *testing.T) {
	t.Parallel()

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

	wrapper := newTestHookWrapper(t, script)
	defer os.Remove(wrapper.Path())

	sh := newTestShell(t)

	if err := sh.RunScript(wrapper.Path(), nil); err != nil {
		t.Fatal(err)
	}

	changes, err := wrapper.Changes()
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(changes.Env, env.FromSlice([]string{"LLAMAS=rock", "Alpacas=are ok"})) {
		t.Fatalf("Unexpected env in %#v", changes.Env)
	}
}

func TestRunningHookDetectsChangedWorkingDirectory(t *testing.T) {
	t.Parallel()

	tempDir, err := ioutil.TempDir("", "hookwrapperdir")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

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

	wrapper := newTestHookWrapper(t, script)
	defer os.Remove(wrapper.Path())

	sh := newTestShell(t)
	if err := sh.Chdir(tempDir); err != nil {
		t.Fatal(err)
	}

	if err := sh.RunScript(wrapper.Path(), nil); err != nil {
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

	if changes.Dir != expected {
		t.Fatalf("Expected working dir of %q, got %q", expected, changes.Dir)
	}
}

func newTestShell(t *testing.T) *shell.Shell {
	sh, err := shell.New()
	if err != nil {
		t.Fatal(err)
	}

	sh.Logger = shell.DiscardLogger
	sh.Writer = ioutil.Discard

	if os.Getenv(`DEBUG_SHELL`) == "1" {
		sh.Logger = shell.TestingLogger{T: t}
	}

	// Windows requires certain env variables to be present
	if runtime.GOOS == "windows" {
		sh.Env = env.FromSlice([]string{
			//			"PATH=" + os.Getenv("PATH"),
			"SystemRoot=" + os.Getenv("SystemRoot"),
			"WINDIR=" + os.Getenv("WINDIR"),
			"COMSPEC=" + os.Getenv("COMSPEC"),
			"PATHEXT=" + os.Getenv("PATHEXT"),
			"TMP=" + os.Getenv("TMP"),
			"TEMP=" + os.Getenv("TEMP"),
		})
	} else {
		sh.Env = env.New()
	}

	return sh
}

func newTestHookWrapper(t *testing.T, script []string) *hookScriptWrapper {
	hookName := "hookwrapper"
	if runtime.GOOS == "windows" {
		hookName += ".bat"
	}

	hookFile, err := shell.TempFileWithExtension(hookName)
	if err != nil {
		t.Fatal(err)
	}

	for _, line := range script {
		if _, err = fmt.Fprintln(hookFile, line); err != nil {
			t.Fatal(err)
		}
	}

	hookFile.Close()

	wrapper, err := newHookScriptWrapper(hookFile.Name())
	if err != nil {
		t.Fatal(err)
	}

	return wrapper
}
