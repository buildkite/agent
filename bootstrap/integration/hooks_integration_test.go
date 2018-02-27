package integration

import (
	"fmt"
	"io/ioutil"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/lox/bintest"
	"github.com/lox/bintest/proxy"
)

func TestEnvironmentVariablesPassBetweenHooks(t *testing.T) {
	t.Parallel()

	tester, err := NewBootstrapTester()
	if err != nil {
		t.Fatal(err)
	}
	defer tester.Close()

	if runtime.GOOS != "windows" {
		var script = []string{
			"#!/bin/bash",
			"export LLAMAS_ROCK=absolutely",
		}

		if err := ioutil.WriteFile(filepath.Join(tester.HooksDir, "environment"),
			[]byte(strings.Join(script, "\n")), 0700); err != nil {
			t.Fatal(err)
		}
	} else {
		var script = []string{
			"@echo off",
			"set LLAMAS_ROCK=absolutely",
		}

		if err := ioutil.WriteFile(filepath.Join(tester.HooksDir, "environment.bat"),
			[]byte(strings.Join(script, "\r\n")), 0700); err != nil {
			t.Fatal(err)
		}
	}

	git := tester.MustMock(t, "git").PassthroughToLocalCommand().Before(func(i bintest.Invocation) error {
		if err := bintest.ExpectEnv(t, i.Env, `MY_CUSTOM_ENV=1`, `LLAMAS_ROCK=absolutely`); err != nil {
			return err
		}
		return nil
	})

	git.Expect().AtLeastOnce().WithAnyArguments()

	tester.ExpectGlobalHook("command").Once().AndExitWith(0).AndCallFunc(func(c *proxy.Call) {
		if err := bintest.ExpectEnv(t, c.Env, `MY_CUSTOM_ENV=1`, `LLAMAS_ROCK=absolutely`); err != nil {
			fmt.Fprintf(c.Stderr, "%v\n", err)
			c.Exit(1)
		}
		c.Exit(0)
	})

	tester.RunAndCheck(t, "MY_CUSTOM_ENV=1")
}

func TestDirectoryPassesBetweenHooks(t *testing.T) {
	t.Parallel()

	tester, err := NewBootstrapTester()
	if err != nil {
		t.Fatal(err)
	}
	defer tester.Close()

	if runtime.GOOS == "windows" {
		t.Skip("Not implemented for windows yet")
	}

	var script = []string{
		"#!/bin/bash",
		"mkdir -p ./mysubdir",
		"export MY_CUSTOM_SUBDIR=$(cd mysubdir; pwd)",
		"cd ./mysubdir",
	}

	if err := ioutil.WriteFile(filepath.Join(tester.HooksDir, "pre-command"), []byte(strings.Join(script, "\n")), 0700); err != nil {
		t.Fatal(err)
	}

	tester.ExpectGlobalHook("command").Once().AndExitWith(0).AndCallFunc(func(c *proxy.Call) {
		if c.GetEnv("MY_CUSTOM_SUBDIR") != c.Dir {
			fmt.Fprintf(c.Stderr, "Expected current dir to be %q, got %q\n", c.GetEnv("MY_CUSTOM_SUBDIR"), c.Dir)
			c.Exit(1)
		}
		c.Exit(0)
	})

	tester.RunAndCheck(t, "MY_CUSTOM_ENV=1")
}

func TestCheckingOutFiresCorrectHooks(t *testing.T) {
	t.Parallel()

	tester, err := NewBootstrapTester()
	if err != nil {
		t.Fatal(err)
	}
	defer tester.Close()

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

	tester, err := NewBootstrapTester()
	if err != nil {
		t.Fatal(err)
	}
	defer tester.Close()

	// run a checkout in our checkout hook, otherwise we won't have local hooks to run
	tester.ExpectGlobalHook("checkout").Once().AndCallFunc(func(c *proxy.Call) {
		out, err := tester.Repo.Execute("clone", "-v", "--", tester.Repo.Path, c.GetEnv(`BUILDKITE_BUILD_CHECKOUT_PATH`))
		fmt.Fprint(c.Stderr, out)
		if err != nil {
			c.Exit(1)
			return
		}
		c.Exit(0)
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

	tester, err := NewBootstrapTester()
	if err != nil {
		t.Fatal(err)
	}
	defer tester.Close()

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

	tester, err := NewBootstrapTester()
	if err != nil {
		t.Fatal(err)
	}
	defer tester.Close()

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

	tester, err := NewBootstrapTester()
	if err != nil {
		t.Fatal(err)
	}
	defer tester.Close()

	tester.ExpectGlobalHook("pre-exit").Once()
	tester.ExpectLocalHook("pre-exit").Once()

	if err = tester.Run(t, "BUILDKITE_COMMAND=false"); err == nil {
		t.Fatal("Expected the bootstrap to fail")
	}

	tester.CheckMocks(t)
}

func TestPreExitHooksFireAfterHookFailures(t *testing.T) {
	t.Parallel()

	var testCases = []struct {
		failingHook         string
		expectGlobalPreExit bool
		expectLocalPreExit  bool
		expectCheckout      bool
		expectArtifacts     bool
	}{
		{"environment", true, false, false, false},
		{"pre-checkout", true, false, false, false},
		{"post-checkout", true, true, true, true},
		{"checkout", true, false, false, false},
		{"pre-command", true, true, true, true},
		{"command", true, true, true, true},
		{"post-command", true, true, true, true},
		{"pre-artifact", true, true, true, false},
		{"post-artifact", true, true, true, true},
	}

	for _, tc := range testCases {
		t.Run(tc.failingHook, func(t *testing.T) {
			t.Parallel()

			tester, err := NewBootstrapTester()
			if err != nil {
				t.Fatal(err)
			}
			defer tester.Close()

			agent := tester.MustMock(t, "buildkite-agent")

			tester.ExpectGlobalHook(tc.failingHook).
				Once().
				AndWriteToStderr("Blargh\n").
				AndExitWith(1)

			if tc.expectCheckout {
				agent.
					Expect("meta-data", "exists", "buildkite:git:commit").
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

			if err = tester.Run(t, "BUILDKITE_ARTIFACT_PATHS=test.txt"); err == nil {
				t.Fatal("Expected the bootstrap to fail")
			}

			tester.CheckMocks(t)
		})
	}
}

func TestNoLocalHooksCalledWhenConfigSet(t *testing.T) {
	t.Parallel()

	tester, err := NewBootstrapTester()
	if err != nil {
		t.Fatal(err)
	}
	defer tester.Close()

	tester.Env = append(tester.Env, "BUILDKITE_NO_LOCAL_HOOKS=true")

	tester.ExpectGlobalHook("pre-command").Once()
	tester.ExpectLocalHook("pre-command").NotCalled()

	if err = tester.Run(t, "BUILDKITE_COMMAND=true"); err == nil {
		t.Fatal("Expected the bootstrap to fail due to local hook being called")
	}

	tester.CheckMocks(t)
}
