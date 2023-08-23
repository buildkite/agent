package integration

import (
	"fmt"
	"os"
	"runtime"
	"testing"

	"github.com/buildkite/agent/v3/experiments"
	"github.com/buildkite/bintest/v3"
)

// We want to support the situation where a user wants a plugin from a particular Git branch, e.g.,
// org/repo#my-dev-feature.  By default, if the Buildkite agent finds a plugin Git clone that
// matches the org, repo and ref, it will not try to pull or update it in any way, meaning that if
// the ref is a branch, and upstream has new commits, they will not get pulled in.  For that, we're
// introducing the BUILDKITE_PLUGINS_ALWAYS_CLONE_FRESH setting, which allows a user to force the
// agent to always make a fresh clone of any plugins.  This integration test and the one after test
// that a plugin modified upstream is treated as expected.  That is, by default, the updates won't
// take effect, but with BUILDKITE_PLUGINS_ALWAYS_CLONE_FRESH set, they will.
func TestModifiedIsolatedPluginNoForcePull(t *testing.T) {
	t.Parallel()

	ctx, _ := experiments.Enable(mainCtx, experiments.IsolatedPluginCheckout)

	tester, err := NewBootstrapTester(ctx)
	if err != nil {
		t.Fatalf("NewBootstrapTester() error = %v", err)
	}
	defer tester.Close()

	// Let's set a fixed location for plugins, otherwise NewBootstrapTester() gives us a random new
	// tempdir every time, which defeats our test.  Later we'll use this pluginsDir for the second
	// test run, too.
	pluginsDir, err := os.MkdirTemp("", "bootstrap-plugins")
	if err != nil {
		t.Fatalf(`os.MkdirTemp("", "bootstrap-plugins") error = %v`, err)
	}
	tester.PluginsDir = pluginsDir

	// There's a bit of machinery in replacePluginPathInEnv to modify only the
	// BUILDKITE_PLUGINS_PATH, leaving the rest of the environment variables NewBootstrapTester()
	// gave us as-is.
	tester.Env = replacePluginPathInEnv(tester.Env, pluginsDir)

	// Create a test plugin that sets an environment variable.
	hooks := map[string][]string{
		"environment": {
			"#!/bin/bash",
			"export OSTRICH_EGGS=quite_large",
		},
	}
	if runtime.GOOS == "windows" {
		hooks = map[string][]string{
			"environment.bat": {
				"@echo off",
				"set OSTRICH_EGGS=quite_large",
			},
		}
	}
	p := createTestPlugin(t, hooks)

	// You may be surprised that we're creating a branch here.  This is so we can test the behaviour
	// when a branch has had new commits added to it.
	p.gitRepository.CreateBranch("something-fixed")
	// To test this, we also set our testPlugin to version "something-fixed", so that the agent will
	// check out that ref.
	p.versionTag = "something-fixed"

	json, err := p.ToJSON()
	if err != nil {
		t.Fatalf("testPlugin.ToJSON() error = %v", err)
	}

	env := []string{
		"BUILDKITE_PLUGINS=" + json,
	}

	tester.ExpectGlobalHook("command").Once().AndExitWith(0).AndCallFunc(func(c *bintest.Call) {
		if err := bintest.ExpectEnv(t, c.Env, "OSTRICH_EGGS=quite_large"); err != nil {
			fmt.Fprintf(c.Stderr, "%v\n", err)
			c.Exit(1)
		} else {
			c.Exit(0)
		}
	})

	tester.RunAndCheck(t, env...)

	// Now, we want to "repeat" the test build, having modified the plugin's contents.
	tester2, err := NewBootstrapTester(ctx)
	if err != nil {
		t.Fatalf("NewBootstrapTester() error = %v", err)
	}
	defer tester2.Close()

	// Same modification of BUILDKITE_PLUGINS_PATH.
	tester2.PluginsDir = pluginsDir
	tester2.Env = replacePluginPathInEnv(tester2.Env, pluginsDir)

	hooks2 := map[string][]string{
		"environment": {
			"#!/bin/bash",
			"export OSTRICH_EGGS=huge_actually",
		},
	}
	if runtime.GOOS == "windows" {
		hooks2 = map[string][]string{
			"environment.bat": {
				"@echo off",
				"set OSTRICH_EGGS=huge_actually",
			},
		}
	}
	modifyTestPlugin(t, hooks2, p)

	tester2.ExpectGlobalHook("command").Once().AndExitWith(0).AndCallFunc(func(c *bintest.Call) {
		if err := bintest.ExpectEnv(t, c.Env, "OSTRICH_EGGS=quite_large"); err != nil {
			fmt.Fprintf(c.Stderr, "%v\n", err)
			c.Exit(1)
		} else {
			c.Exit(0)
		}
	})

	tester2.RunAndCheck(t, env...)
}

// As described above the previous integration test, this time we want to run the build both before
// and after modifying a plugin's source, but this time with BUILDKITE_PLUGINS_ALWAYS_CLONE_FRESH
// set to true.  So, we expect the upstream plugin changes to take effect on our second build.
func TestModifiedIsolatedPluginWithForcePull(t *testing.T) {
	t.Parallel()

	ctx, _ := experiments.Enable(mainCtx, experiments.IsolatedPluginCheckout)

	tester, err := NewBootstrapTester(ctx)
	if err != nil {
		t.Fatalf("NewBootstrapTester() error = %v", err)
	}
	defer tester.Close()

	// Let's set a fixed location for plugins, otherwise it'll be a random new tempdir every time
	// which defeats our test.
	pluginsDir, err := os.MkdirTemp("", "bootstrap-plugins")
	if err != nil {
		t.Fatalf(`os.MkdirTemp("", "bootstrap-plugins") error = %v`, err)
	}

	// Same resetting code for BUILDKITE_PLUGINS_PATH as in the previous test
	tester.PluginsDir = pluginsDir
	tester.Env = replacePluginPathInEnv(tester.Env, pluginsDir)

	hooks := map[string][]string{
		"environment": {
			"#!/bin/bash",
			"export OSTRICH_EGGS=quite_large",
		},
	}
	if runtime.GOOS == "windows" {
		hooks = map[string][]string{
			"environment.bat": {
				"@echo off",
				"set OSTRICH_EGGS=quite_large",
			},
		}
	}
	p := createTestPlugin(t, hooks)

	// Same branch-name jiggery pokery as in the previous integration test
	p.gitRepository.CreateBranch("something-fixed")
	p.versionTag = "something-fixed"

	json, err := p.ToJSON()
	if err != nil {
		t.Fatalf("testPlugin.ToJSON() error = %v", err)
	}

	env := []string{
		"BUILDKITE_PLUGINS=" + json,
	}

	tester.ExpectGlobalHook("command").Once().AndExitWith(0).AndCallFunc(func(c *bintest.Call) {
		if err := bintest.ExpectEnv(t, c.Env, "OSTRICH_EGGS=quite_large"); err != nil {
			fmt.Fprintf(c.Stderr, "%v\n", err)
			c.Exit(1)
		} else {
			c.Exit(0)
		}
	})

	tester.RunAndCheck(t, env...)

	tester2, err := NewBootstrapTester(ctx)
	if err != nil {
		t.Fatalf("NewBootstrapTester() error = %v", err)
	}
	defer tester2.Close()

	tester2.PluginsDir = pluginsDir
	tester2.Env = replacePluginPathInEnv(tester2.Env, pluginsDir)

	// Notice the all-important setting, BUILDKITE_PLUGINS_ALWAYS_CLONE_FRESH, being enabled.
	tester2.Env = append(tester2.Env, "BUILDKITE_PLUGINS_ALWAYS_CLONE_FRESH=true")

	hooks2 := map[string][]string{
		"environment": {
			"#!/bin/bash",
			"export OSTRICH_EGGS=huge_actually",
		},
	}
	if runtime.GOOS == "windows" {
		hooks2 = map[string][]string{
			"environment.bat": {
				"@echo off",
				"set OSTRICH_EGGS=huge_actually",
			},
		}
	}
	modifyTestPlugin(t, hooks2, p)

	// This time, we expect the value of OSTRICH_EGGS to have changed compared to the first test
	// run.
	tester2.ExpectGlobalHook("command").Once().AndExitWith(0).AndCallFunc(func(c *bintest.Call) {
		if err := bintest.ExpectEnv(t, c.Env, "OSTRICH_EGGS=huge_actually"); err != nil {
			fmt.Fprintf(c.Stderr, "%v\n", err)
			c.Exit(1)
		} else {
			c.Exit(0)
		}
	})

	tester2.RunAndCheck(t, env...)
}
