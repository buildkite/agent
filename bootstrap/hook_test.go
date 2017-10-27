package bootstrap

import (
	"fmt"
	"io/ioutil"
	"os"
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

	afterEnv, err := wrapper.ChangedEnvironment()
	if err != nil {
		t.Fatal(err)
	}

	if afterEnv.Length() != 3 {
		t.Fatalf("Expected 3 env vars, got %d: %#v", afterEnv.Length(), afterEnv)
	}

	if actual := afterEnv.Get("LLAMAS"); actual != "rock" {
		t.Fatalf("Expected %q, got %q", "rock", actual)
	}

	if actual := afterEnv.Get("Alpacas"); actual != "are ok" {
		t.Fatalf("Expected %q, got %q", "are ok", actual)
	}
}

func newTestShell(t *testing.T) *shell.Shell {
	sh, err := shell.New()
	if err != nil {
		t.Fatal(err)
	}

	sh.Logger = shell.DiscardLogger
	sh.Writer = ioutil.Discard
	sh.Env = env.FromSlice([]string{})

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
