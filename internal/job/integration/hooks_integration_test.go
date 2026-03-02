package integration

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/buildkite/agent/v3/internal/job"
	"github.com/buildkite/agent/v3/internal/shell"
	"github.com/buildkite/bintest/v3"
)

func TestEnvironmentVariablesPassBetweenHooks(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

	filename := "environment"
	script := []string{
		"#!/usr/bin/env bash",
		"export LLAMAS_ROCK=absolutely",
	}
	if runtime.GOOS == "windows" {
		filename = "environment.bat"
		script = []string{
			"@echo off",
			"set LLAMAS_ROCK=absolutely",
		}
	}

	if err := os.WriteFile(filepath.Join(tester.HooksDir, filename), []byte(strings.Join(script, "\n")), 0o700); err != nil {
		t.Fatalf("os.WriteFile(%q, script, 0o700) = %v", filename, err)
	}

	git := tester.MustMock(t, "git").PassthroughToLocalCommand().Before(func(i bintest.Invocation) error {
		return bintest.ExpectEnv(t, i.Env, "MY_CUSTOM_ENV=1", "LLAMAS_ROCK=absolutely")
	})

	git.Expect().AtLeastOnce().WithAnyArguments()

	tester.ExpectGlobalHook("command").Once().AndExitWith(0).AndCallFunc(func(c *bintest.Call) {
		if err := bintest.ExpectEnv(t, c.Env, "MY_CUSTOM_ENV=1", "LLAMAS_ROCK=absolutely"); err != nil {
			fmt.Fprintf(c.Stderr, "%v\n", err) //nolint:errcheck // test helper; write error is non-actionable
			c.Exit(1)
		} else {
			c.Exit(0)
		}
	})

	tester.RunAndCheck(t, "MY_CUSTOM_ENV=1")
}

func TestHooksCanUnsetEnvironmentVariables(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

	preCmdFile, postCmdFile := "pre-command", "post-command"
	preCommand := []string{
		"#!/usr/bin/env bash",
		"export LLAMAS_ROCK=absolutely",
	}
	postCommand := []string{
		"#!/usr/bin/env bash",
		"unset LLAMAS_ROCK",
	}

	if runtime.GOOS == "windows" {
		preCmdFile, postCmdFile = "pre-command.bat", "post-command.bat"
		preCommand = []string{
			"@echo off",
			"set LLAMAS_ROCK=absolutely",
		}
		postCommand = []string{
			"@echo off",
			"set LLAMAS_ROCK=",
		}
	}

	if err := os.WriteFile(filepath.Join(tester.HooksDir, preCmdFile), []byte(strings.Join(preCommand, "\n")), 0o700); err != nil {
		t.Fatalf("os.WriteFile(%q, preCommand, 0o700) = %v", preCmdFile, err)
	}

	if err := os.WriteFile(filepath.Join(tester.HooksDir, postCmdFile), []byte(strings.Join(postCommand, "\n")), 0o700); err != nil {
		t.Fatalf("os.WriteFile(%q, postCommand, 0o700) = %v", postCmdFile, err)
	}

	tester.ExpectGlobalHook("command").Once().AndExitWith(0).AndCallFunc(func(c *bintest.Call) {
		if c.GetEnv("LLAMAS_ROCK") != "absolutely" {
			fmt.Fprintf(c.Stderr, "Expected command hook to have environment variable LLAMAS_ROCK be %q, got %q\n", "absolutely", c.GetEnv("LLAMAS_ROCK")) //nolint:errcheck // test helper; write error is non-actionable
			c.Exit(1)
		} else {
			c.Exit(0)
		}
	})

	tester.ExpectGlobalHook("pre-exit").Once().AndExitWith(0).AndCallFunc(func(c *bintest.Call) {
		if c.GetEnv("LLAMAS_ROCK") != "" {
			fmt.Fprintf(c.Stderr, "Expected pre-exit hook to have environment variable LLAMAS_ROCK be empty, got %q\n", c.GetEnv("LLAMAS_ROCK")) //nolint:errcheck // test helper; write error is non-actionable
			c.Exit(1)
		} else {
			c.Exit(0)
		}
	})

	tester.RunAndCheck(t, "MY_CUSTOM_ENV=1")
}

func TestDirectoryPassesBetweenHooks(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

	if runtime.GOOS == "windows" {
		t.Skip("Not implemented for windows yet")
	}

	script := []string{
		"#!/usr/bin/env bash",
		"mkdir -p ./mysubdir",
		"export MY_CUSTOM_SUBDIR=$(cd mysubdir; pwd)",
		"cd ./mysubdir",
	}

	if err := os.WriteFile(filepath.Join(tester.HooksDir, "pre-command"), []byte(strings.Join(script, "\n")), 0o700); err != nil {
		t.Fatalf("os.WriteFile(pre-command, script, 0o700) = %v", err)
	}

	tester.ExpectGlobalHook("command").Once().AndExitWith(0).AndCallFunc(func(c *bintest.Call) {
		if c.GetEnv("MY_CUSTOM_SUBDIR") != c.Dir {
			fmt.Fprintf(c.Stderr, "Expected current dir to be %q, got %q\n", c.GetEnv("MY_CUSTOM_SUBDIR"), c.Dir) //nolint:errcheck // test helper; write error is non-actionable
			c.Exit(1)
		} else {
			c.Exit(0)
		}
	})

	tester.RunAndCheck(t, "MY_CUSTOM_ENV=1")
}

func TestDirectoryPassesBetweenHooksIgnoredUnderExit(t *testing.T) {
	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

	if runtime.GOOS == "windows" {
		t.Skip("Not implemented for windows yet")
	}

	script := []string{
		"#!/usr/bin/env bash",
		"mkdir -p ./mysubdir",
		"export MY_CUSTOM_SUBDIR=$(cd mysubdir; pwd)",
		"cd ./mysubdir",
		"exit 0",
	}

	if err := os.WriteFile(filepath.Join(tester.HooksDir, "pre-command"), []byte(strings.Join(script, "\n")), 0o700); err != nil {
		t.Fatalf("os.WriteFile(pre-command, script, 0o700) = %v", err)
	}

	tester.ExpectGlobalHook("command").Once().AndExitWith(0).AndCallFunc(func(c *bintest.Call) {
		if c.GetEnv("BUILDKITE_BUILD_CHECKOUT_PATH") != c.Dir {
			fmt.Fprintf(c.Stderr, "Expected current dir to be %q, got %q\n", c.GetEnv("BUILDKITE_BUILD_CHECKOUT_PATH"), c.Dir) //nolint:errcheck // test helper; write error is non-actionable
			c.Exit(1)
		} else {
			c.Exit(0)
		}
	})

	tester.RunAndCheck(t, "MY_CUSTOM_ENV=1")
}

func TestCheckingOutFiresCorrectHooks(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

	tester.ExpectGlobalHook("environment").Once()
	tester.ExpectLocalHook("environment").NotCalled()
	tester.ExpectGlobalHook("pre-checkout").Once()
	tester.ExpectLocalHook("pre-checkout").NotCalled()
	tester.ExpectGlobalHook("post-checkout").Once()
	tester.ExpectLocalHook("post-checkout").Once()
	tester.ExpectGlobalHook("pre-command").Once()
	tester.ExpectLocalHook("pre-command").Once()
	tester.ExpectGlobalHook("command").Once().AndExitWith(0).AndWriteToStdout("Success!\n")
	tester.ExpectGlobalHook("post-command").Once()
	tester.ExpectLocalHook("post-command").Once()
	tester.ExpectGlobalHook("pre-artifact").NotCalled()
	tester.ExpectLocalHook("pre-artifact").NotCalled()
	tester.ExpectGlobalHook("post-artifact").NotCalled()
	tester.ExpectLocalHook("post-artifact").NotCalled()
	tester.ExpectGlobalHook("pre-exit").Once()
	tester.ExpectLocalHook("pre-exit").Once()

	tester.RunAndCheck(t)
}

func TestReplacingCheckoutHook(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

	// run a checkout in our checkout hook, otherwise we won't have local hooks to run
	tester.ExpectGlobalHook("checkout").Once().AndCallFunc(func(c *bintest.Call) {
		out, err := tester.Repo.Execute("clone", "-v", "--", tester.Repo.Path, c.GetEnv("BUILDKITE_BUILD_CHECKOUT_PATH"))
		fmt.Fprint(c.Stderr, out) //nolint:errcheck // test helper; write error is non-actionable
		if err != nil {
			c.Exit(1)
		} else {
			c.Exit(0)
		}
	})

	tester.ExpectGlobalHook("pre-checkout").Once()
	tester.ExpectGlobalHook("post-checkout").Once()
	tester.ExpectLocalHook("post-checkout").Once()
	tester.ExpectGlobalHook("pre-exit").Once()
	tester.ExpectLocalHook("pre-exit").Once()

	tester.RunAndCheck(t)
}

func TestReplacingGlobalCommandHook(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

	tester.ExpectGlobalHook("command").Once().AndExitWith(0)

	tester.ExpectGlobalHook("environment").Once()
	tester.ExpectGlobalHook("pre-checkout").Once()
	tester.ExpectGlobalHook("post-checkout").Once()
	tester.ExpectLocalHook("post-checkout").Once()
	tester.ExpectGlobalHook("pre-command").Once()
	tester.ExpectLocalHook("pre-command").Once()
	tester.ExpectGlobalHook("post-command").Once()
	tester.ExpectLocalHook("post-command").Once()
	tester.ExpectGlobalHook("pre-exit").Once()
	tester.ExpectLocalHook("pre-exit").Once()

	tester.RunAndCheck(t)
}

func TestReplacingLocalCommandHook(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

	tester.ExpectLocalHook("command").Once().AndExitWith(0)
	tester.ExpectGlobalHook("command").NotCalled()

	tester.ExpectGlobalHook("environment").Once()
	tester.ExpectGlobalHook("pre-checkout").Once()
	tester.ExpectGlobalHook("post-checkout").Once()
	tester.ExpectLocalHook("post-checkout").Once()
	tester.ExpectGlobalHook("pre-command").Once()
	tester.ExpectLocalHook("pre-command").Once()
	tester.ExpectGlobalHook("post-command").Once()
	tester.ExpectLocalHook("post-command").Once()
	tester.ExpectGlobalHook("pre-exit").Once()
	tester.ExpectLocalHook("pre-exit").Once()

	tester.RunAndCheck(t)
}

func TestPreExitHooksFireAfterCommandFailures(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

	tester.ExpectGlobalHook("pre-exit").Once()
	tester.ExpectLocalHook("pre-exit").Once()

	if err := tester.Run(t, "BUILDKITE_COMMAND=false"); err == nil {
		t.Fatalf("tester.Run(t, BUILDKITE_COMMAND=false) = %v, want non-nil error", err)
	}

	tester.CheckMocks(t)
}

func TestPreExitHooksDoesNotFireWithoutCommandPhase(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

	tester.ExpectGlobalHook("pre-exit").NotCalled()
	tester.ExpectLocalHook("pre-exit").NotCalled()

	tester.RunAndCheck(t, "BUILDKITE_BOOTSTRAP_PHASES=plugin,checkout")
}

func TestPreExitHooksFireAfterHookFailures(t *testing.T) {
	t.Parallel()

	ctx := mainCtx

	testCases := []struct {
		failingHook         string
		expectGlobalPreExit bool
		expectLocalPreExit  bool
		expectCheckout      bool
		expectArtifacts     bool
	}{
		{
			failingHook:         "environment",
			expectGlobalPreExit: true,
			expectLocalPreExit:  false,
			expectCheckout:      false,
			expectArtifacts:     false,
		},
		{
			failingHook:         "pre-checkout",
			expectGlobalPreExit: true,
			expectLocalPreExit:  false,
			expectCheckout:      false,
			expectArtifacts:     false,
		},
		{
			failingHook:         "post-checkout",
			expectGlobalPreExit: true,
			expectLocalPreExit:  true,
			expectCheckout:      true,
			expectArtifacts:     false,
		},
		{
			failingHook:         "checkout",
			expectGlobalPreExit: true,
			expectLocalPreExit:  false,
			expectCheckout:      false,
			expectArtifacts:     false,
		},
		{
			failingHook:         "pre-command",
			expectGlobalPreExit: true,
			expectLocalPreExit:  true,
			expectCheckout:      true,
			expectArtifacts:     true,
		},
		{
			failingHook:         "command",
			expectGlobalPreExit: true,
			expectLocalPreExit:  true,
			expectCheckout:      true,
			expectArtifacts:     true,
		},
		{
			failingHook:         "post-command",
			expectGlobalPreExit: true,
			expectLocalPreExit:  true,
			expectCheckout:      true,
			expectArtifacts:     true,
		},
		{
			failingHook:         "pre-artifact",
			expectGlobalPreExit: true,
			expectLocalPreExit:  true,
			expectCheckout:      true,
			expectArtifacts:     false,
		},
		{
			failingHook:         "post-artifact",
			expectGlobalPreExit: true,
			expectLocalPreExit:  true,
			expectCheckout:      true,
			expectArtifacts:     true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.failingHook, func(t *testing.T) {
			t.Parallel()

			tester, err := NewExecutorTester(ctx)
			if err != nil {
				t.Fatalf("NewExecutorTester() error = %v", err)
			}
			defer tester.Close() //nolint:errcheck // best-effort cleanup in test

			agent := tester.MockAgent(t)

			tester.ExpectGlobalHook(tc.failingHook).
				Once().
				AndWriteToStderr("Blargh\n").
				AndExitWith(1)

			if tc.expectCheckout {
				agent.
					Expect("meta-data", "exists", job.CommitMetadataKey).
					Once().
					AndExitWith(0)
			}

			if tc.expectGlobalPreExit {
				tester.ExpectGlobalHook("pre-exit").Once()
			} else {
				tester.ExpectGlobalHook("pre-exit").NotCalled()
			}

			if tc.expectLocalPreExit {
				tester.ExpectLocalHook("pre-exit").Once()
			} else {
				tester.ExpectGlobalHook("pre-exit").NotCalled()
			}

			if tc.expectArtifacts {
				agent.
					Expect("artifact", "upload", "test.txt").
					AndExitWith(0)
			}

			if err := tester.Run(t, "BUILDKITE_ARTIFACT_PATHS=test.txt"); err == nil {
				t.Fatalf("tester.Run(t, BUILDKITE_ARTIFACT_PATHS=test.txt) = %v, want non-nil error", err)
			}

			tester.CheckMocks(t)
		})
	}
}

func TestNoLocalHooksCalledWhenConfigSet(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

	tester.Env = append(tester.Env, "BUILDKITE_NO_LOCAL_HOOKS=true")

	tester.ExpectGlobalHook("pre-command").Once()
	tester.ExpectLocalHook("pre-command").NotCalled()

	if err = tester.Run(t, "BUILDKITE_COMMAND=true"); err == nil {
		t.Fatalf("tester.Run(t, BUILDKITE_COMMAND=true) = %v, want non-nil error", err)
	}

	tester.CheckMocks(t)
}

func TestExitCodesPropagateOutFromGlobalHooks(t *testing.T) {
	t.Parallel()

	ctx := mainCtx

	for _, hook := range []string{
		"environment",
		"pre-checkout",
		"post-checkout",
		"checkout",
		"pre-command",
		"command",
		"post-command",
		"pre-exit",
		// "pre-artifact",
		// "post-artifact",
	} {
		t.Run(hook, func(t *testing.T) {
			tester, err := NewExecutorTester(ctx)
			if err != nil {
				t.Fatalf("NewExecutorTester() error = %v", err)
			}
			defer tester.Close() //nolint:errcheck // best-effort cleanup in test

			tester.ExpectGlobalHook(hook).Once().AndExitWith(5)

			err = tester.Run(t)

			if err == nil {
				t.Fatalf("tester.Run(t) = %v, want non-nil error", err)
			}
			if got, want := shell.ExitCode(err), 5; got != want {
				t.Fatalf("shell.GetExitCode(%v) = %d, want %d", err, got, want)
			}

			tester.CheckMocks(t)
		})
	}
}

func TestPreExitHooksFireAfterCancel(t *testing.T) {
	t.Parallel()

	// TODO: Why is this test skipped on windows and darwin?
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		t.Skip()
	}

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

	tester.ExpectGlobalHook("pre-exit").Once()
	tester.ExpectLocalHook("pre-exit").Once()

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		if err := tester.Run(t, "BUILDKITE_COMMAND=sleep 5"); err == nil {
			t.Errorf(`tester.Run(t, "BUILDKITE_COMMAND=sleep 5") = %v, want non-nil error`, err)
		}
		t.Logf("Command finished")
	}()

	time.Sleep(time.Millisecond * 500)
	tester.Cancel() //nolint:errcheck // best-effort cancel in test

	t.Logf("Waiting for command to finish")
	wg.Wait()

	tester.CheckMocks(t)
}

func TestPolyglotScriptHooksCanBeRun(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("script hooks aren't supported on windows")
	}

	path, err := exec.LookPath("ruby")
	if err != nil {
		t.Fatalf("error finding path to ruby executable: %v", err)
	}

	if path == "" {
		t.Fatal("ruby not found in $PATH. This test requires ruby to be installed on the host")
	}

	ctx := mainCtx

	tester, err := NewExecutorTester(ctx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

	filename := "environment"
	script := []string{
		"#!/usr/bin/env ruby",
		`puts "ohai, it's ruby!"`,
	}

	if err := os.WriteFile(filepath.Join(tester.HooksDir, filename), []byte(strings.Join(script, "\n")), 0o755); err != nil {
		t.Fatalf("os.WriteFile(%q, script, 0o755) = %v", filename, err)
	}

	tester.RunAndCheck(t)

	if !strings.Contains(tester.Output, "ohai, it's ruby!") {
		t.Fatalf("tester.Output %q does not contain expected output: %q", tester.Output, "ohai, it's ruby!")
	}
}

func TestPolyglotBinaryHooksCanBeRun(t *testing.T) {
	t.Parallel()

	ctx := mainCtx

	tester, err := NewExecutorTester(ctx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

	// We build a binary as part of this test so that we can produce a binary hook
	// Forgive me for my sins, RSC, but it's better than the alternatives.
	// The code that we're building is in ./test-binary-hook/main.go, it's pretty straightforward.

	t.Logf("Building test-binary-hook")
	hookPath := filepath.Join(tester.HooksDir, "environment")

	if runtime.GOOS == "windows" {
		hookPath += ".exe"
	}

	output, err := exec.Command("go", "build", "-o", hookPath, "./test-binary-hook").CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to build test-binary-hook: %v, output: %s", err, string(output))
	}

	tester.ExpectGlobalHook("post-command").Once().AndExitWith(0).AndCallFunc(func(c *bintest.Call) {
		// Set via ./test-binary-hook/main.go
		if c.GetEnv("OCEAN") != "Pacífico" {
			fmt.Fprintf(c.Stderr, "Expected OCEAN to be Pacífico, got %q", c.GetEnv("OCEAN")) //nolint:errcheck // test helper; write error is non-actionable
			c.Exit(1)
		} else {
			c.Exit(0)
		}
	})

	tester.RunAndCheck(t)

	if !strings.Contains(tester.Output, "hi there from golang 🌊") {
		t.Fatalf("tester.Output %s does not contain expected output: %q", tester.Output, "hi there from golang 🌊")
	}
}
