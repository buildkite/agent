package hook_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/buildkite/agent/v4/env"
	"github.com/buildkite/agent/v4/internal/job/hook"
	"github.com/buildkite/agent/v4/internal/shell"
	"github.com/buildkite/agent/v4/internal/tempfile"
	"github.com/google/go-cmp/cmp"
)

type hookTestCase struct {
	name, os, hook string
}

func TestRunningHookDetectsChangedEnvironment(t *testing.T) {
	t.Parallel()

	testCases := []hookTestCase{
		{
			name: "hook",
			os:   "linux",
			hook: `#!/bin/sh
export LLAMAS=rock
export Alpacas='are ok'
echo hello world
`,
		},
		{
			name: "hook.sh",
			os:   "linux",
			hook: `#!/bin/sh
export LLAMAS=rock
export Alpacas='are ok'
echo hello world
`,
		},
	}

	if runtime.GOOS == "windows" {
		testCases = append(testCases,
			hookTestCase{
				name: "hook.bat",
				os:   "windows",
				hook: `@echo off
set LLAMAS=rock
set Alpacas=are ok
echo hello world
`,
			},
			hookTestCase{
				name: "hook.ps1",
				os:   "windows",
				hook: `$env:LLAMAS = "rock"
$env:Alpacas = "are ok"
echo "hello world"
`,
			},
		)
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()

			hookFilename := writeTestHook(t, tc.name, tc.hook)
			wrapper, err := hook.NewWrapper(hook.WithPath(hookFilename), hook.WithOS(tc.os))
			if err != nil {
				t.Fatalf("failed to create hook wrapper: %v", err)
			}

			sh := shell.NewTestShell(t)

			script, err := sh.Script(wrapper.Path(), "")
			if err != nil {
				t.Fatalf("sh.Script(%q) = %v", wrapper.Path(), err)
			}
			if err := script.Run(ctx, shell.ShowPrompt(false)); err != nil {
				t.Fatalf("script(%q).Run(ctx, shell.ShowPrompt(false)) = %v", wrapper.Path(), err)
			}

			changes, err := wrapper.Changes()
			if err != nil {
				t.Fatalf("wrapper.Changes() = %v", err)
			}

			// Windows’ batch 'SET >' normalises environment variables case so we apply
			// the 'expected' and 'actual' diffs to a blank Environment which handles
			// case normalisation for us
			expected := env.New()
			expected.Apply(env.Diff{
				Added: map[string]string{
					"LLAMAS":  "rock",
					"Alpacas": "are ok",
				},
				Changed: map[string]env.DiffPair{},
				Removed: map[string]struct{}{},
			})

			actual := env.New()
			actual.Apply(changes.Diff)

			// The strict equals check here also ensures we aren't bubbling up the
			// internal BUILDKITE_HOOK_EXIT_STATUS and BUILDKITE_HOOK_WORKING_DIR
			// environment variables
			if diff := cmp.Diff(actual.Dump(), expected.Dump()); diff != "" {
				t.Fatalf("actual.Dump() diff (-got +want):\n%s", diff)
			}
		})
	}
}

func TestRunningHookDetectsChangedWorkingDirectory(t *testing.T) {
	t.Parallel()

	testCases := []hookTestCase{
		{
			name: "hook",
			os:   "linux",
			hook: `#!/bin/sh
mkdir changed-working-dir
cd changed-working-dir
echo hello world
`,
		},
		{
			name: "hook.sh",
			os:   "linux",
			hook: `#!/bin/sh
mkdir changed-working-dir
cd changed-working-dir
echo hello world
`,
		},
	}

	if runtime.GOOS == "windows" {
		testCases = []hookTestCase{
			{
				name: "hook.bat",
				os:   "windows",
				hook: `@echo off
mkdir changed-working-dir
cd changed-working-dir
echo hello world
`,
			},
			{
				name: "hook.ps1",
				os:   "windows",
				hook: `mkdir changed-working-dir
cd changed-working-dir
echo hello world
`,
			},
		}
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()

			hookFilename := writeTestHook(t, tc.name, tc.hook)
			wrapper, err := hook.NewWrapper(hook.WithPath(hookFilename), hook.WithOS(tc.os))
			if err != nil {
				t.Fatalf("failed to create hook wrapper: %v", err)
			}

			sh := shell.NewTestShell(t)

			hookWorkingDir, err := os.MkdirTemp("", "test-hook-working-dir")
			if err != nil {
				t.Fatalf(`os.MkdirTemp("", "test-hook-working-dir") error = %v`, err)
			}

			err = sh.Chdir(hookWorkingDir)
			if err != nil {
				t.Fatalf("sh.Chdir(%q) = %v", hookWorkingDir, err)
			}

			script, err := sh.Script(wrapper.Path(), "")
			if err != nil {
				t.Fatalf("sh.Script(%q) = %v", wrapper.Path(), err)
			}
			if err := script.Run(ctx, shell.ShowPrompt(false)); err != nil {
				t.Fatalf("script(%q).Run(ctx, shell.ShowPrompt(false)) = %v", wrapper.Path(), err)
			}

			changes, err := wrapper.Changes()
			if err != nil {
				t.Fatalf("wrapper.Changes() = %v", err)
			}

			absWorkingDir := filepath.Join(hookWorkingDir, "changed-working-dir")

			expectedWorkingDir, err := filepath.EvalSymlinks(absWorkingDir)
			if err != nil {
				t.Fatalf("filepath.EvalSymlinks(%q) error = %v", absWorkingDir, err)
			}

			afterWd, err := changes.GetAfterWd()
			if err != nil {
				t.Fatalf("changes.GetAfterWd() = %v", err)
			}

			actualWorkingDir, err := filepath.EvalSymlinks(afterWd)
			if err != nil {
				t.Fatalf("filepath.EvalSymlinks(%q) error = %v", afterWd, err)
			}

			if got, want := actualWorkingDir, expectedWorkingDir; got != want {
				t.Fatalf("filepath.EvalSymlinks(%q) = %q, want %q", afterWd, got, want)
			}
		})
	}
}

func TestScriptWrapperFailsOnHookWithInvalidShebang(t *testing.T) {
	t.Parallel()

	hookFilename := writeTestHook(t, "hook", "#!/usr/bin/env ruby\nputs 'hello world'")

	_, err := hook.NewWrapper(
		hook.WithPath(hookFilename),
		hook.WithOS("linux"),
	)
	if want := `scriptwrapper tried to wrap hook with invalid shebang: "#!/usr/bin/env ruby"`; err == nil || err.Error() != want {
		t.Fatalf("hook.NewWrapper(hook.WithPath(hookFilename), hook.WithOS(\"linux\")) error = %v, want error with message %q", err, want)
	}
}

func writeTestHook(t *testing.T, fileName, content string) string {
	t.Helper()

	tempFile, err := tempfile.New(
		tempfile.WithName(fileName),
		tempfile.KeepingExtension(),
		tempfile.WithPerms(0o700),
	)
	if err != nil {
		t.Fatalf("failed to create temp file with name %q", fileName)
	}

	t.Cleanup(func() {
		if tempFile == nil {
			return
		}

		cerr := tempFile.Close()
		if !errors.Is(cerr, os.ErrClosed) {
			if got := cerr == nil; !got {
				t.Errorf("failed to close temp file %q: %v", tempFile.Name(), cerr)
			}
		}

		rerr := os.Remove(tempFile.Name())
		if got := rerr == nil; !got {
			t.Errorf("failed to remove temp file %q: %v", tempFile.Name(), rerr)
		}
	})

	_, err = io.WriteString(tempFile, content)
	if err != nil {
		t.Fatalf("failed to write to temp file %q", tempFile.Name())
	}

	err = tempFile.Close()
	if err != nil {
		t.Fatalf("failed to close temp file %q", tempFile.Name())
	}

	return tempFile.Name()
}
