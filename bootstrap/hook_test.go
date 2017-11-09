package bootstrap

import (
	"io"
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
	if runtime.GOOS == "windows" {
		t.Skipf("Not tested on windows yet")
	}

	t.Parallel()

	wrapper := newTestHookWrapper(t, []string{
		"#!/bin/bash",
		"export LLAMAS=rock",
		"export Alpacas=\"are ok\"",
		"echo hello world",
	})
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
	if runtime.GOOS == "windows" {
		t.Skipf("Not tested on windows yet")
	}

	t.Parallel()

	tempDir, err := ioutil.TempDir("", "hookwrapperdir")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	wrapper := newTestHookWrapper(t, []string{
		"#!/bin/bash",
		"mkdir mysubdir",
		"cd mysubdir",
		"echo hello world",
	})
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
	sh.Env = env.FromSlice([]string{})

	return sh
}

func newTestHookWrapper(t *testing.T, script []string) *hookScriptWrapper {
	hookFile, err := ioutil.TempFile("", "hookwrapper")
	if err != nil {
		t.Fatal(err)
	}

	for _, line := range script {
		if _, err = io.WriteString(hookFile, line+"\n"); err != nil {
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
