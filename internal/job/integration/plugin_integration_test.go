package integration

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/internal/shell"
	"github.com/buildkite/bintest/v3"
	"gotest.tools/v3/assert"
)

func TestRunningPlugins(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

	pluginMock := tester.MustMock(t, "my-plugin")

	hooks := map[string][]string{
		"environment": {
			"#!/usr/bin/env bash",
			"export LLAMAS_ROCK=absolutely",
			pluginMock.Path + " testing",
		},
	}
	if runtime.GOOS == "windows" {
		hooks = map[string][]string{
			"environment.bat": {
				"@echo off",
				"set LLAMAS_ROCK=absolutely",
				pluginMock.Path + " testing",
			},
		}
	}

	p := createTestPlugin(t, hooks)

	json, err := p.ToJSON()
	if err != nil {
		t.Fatalf("testPlugin.ToJSON() error = %v", err)
	}

	env := []string{
		"MY_CUSTOM_ENV=1",
		"BUILDKITE_PLUGINS=" + json,
	}

	pluginMock.Expect("testing").Once().AndCallFunc(func(c *bintest.Call) {
		if err := bintest.ExpectEnv(t, c.Env, "MY_CUSTOM_ENV=1", "LLAMAS_ROCK=absolutely"); err != nil {
			fmt.Fprintf(c.Stderr, "%v\n", err) //nolint:errcheck // test helper; write error is non-actionable
			c.Exit(1)
		} else {
			c.Exit(0)
		}
	})

	tester.ExpectGlobalHook("command").Once().AndExitWith(0).AndCallFunc(func(c *bintest.Call) {
		if err := bintest.ExpectEnv(t, c.Env, "MY_CUSTOM_ENV=1", "LLAMAS_ROCK=absolutely"); err != nil {
			fmt.Fprintf(c.Stderr, "%v\n", err) //nolint:errcheck // test helper; write error is non-actionable
			c.Exit(1)
		} else {
			c.Exit(0)
		}
	})

	tester.RunAndCheck(t, env...)
}

func TestExitCodesPropagateOutFromPlugins(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

	hooks := map[string][]string{
		"environment": {
			"#!/usr/bin/env bash",
			"exit 5",
		},
	}
	if runtime.GOOS == "windows" {
		hooks = map[string][]string{
			"environment.bat": {
				"@echo off",
				"exit 5",
			},
		}
	}

	p := createTestPlugin(t, hooks)

	json, err := p.ToJSON()
	if err != nil {
		t.Fatalf("testPlugin.ToJSON() error = %v", err)
	}

	env := []string{
		"BUILDKITE_PLUGINS=" + json,
	}

	err = tester.Run(t, env...)

	if err == nil {
		t.Fatalf("tester.Run(t, %v) = %v, want non-nil error", env, err)
	}
	if got, want := shell.ExitCode(err), 5; got != want {
		t.Fatalf("shell.GetExitCode(%v) = %d, want %d", err, got, want)
	}

	tester.CheckMocks(t)
}

func TestMalformedPluginNamesDontCrashBootstrap(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

	env := []string{
		`BUILDKITE_PLUGINS=["sdgmdgn.@$!sdf,asdf#llamas"]`,
	}

	if err := tester.Run(t, env...); err == nil {
		t.Fatalf("tester.Run(t, %v) = %v, want non-nil error", env, err)
	}

	tester.CheckMocks(t)
}

// A job may have multiple plugins that provide multiple hooks of a given type.
// For a while (late 2019 / early 2020) we disallowed duplicate checkout and
// command hooks from plugins; only the first would execute.  We since decided
// to roll that back and permit, for example, multiple checkout plugin hooks.
func TestOverlappingPluginHooks(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

	var testPlugins []*testPlugin
	var pluginMocks []*bintest.Mock
	setupMock := func(name string, hooks ...string) *bintest.Mock {
		pluginMock := tester.MustMock(t, name)
		fakeHooks := make(map[string][]string)
		for _, hook := range hooks {
			if runtime.GOOS == "windows" {
				fakeHooks[hook+".bat"] = []string{
					"@echo off",
					pluginMock.Path + " " + hook,
				}
			} else {
				fakeHooks[hook] = []string{
					"#!/usr/bin/env bash",
					pluginMock.Path + " " + hook,
				}
			}
		}
		testPlugins = append(testPlugins, createTestPlugin(t, fakeHooks))
		pluginMocks = append(pluginMocks, pluginMock)
		return pluginMock
	}

	mockA := setupMock("plugin-a", "environment")
	mockA.Expect("environment").Once()

	mockB := setupMock("plugin-b", "environment", "checkout")
	mockB.Expect("environment").Once()
	mockB.Expect("checkout").Once()

	mockC := setupMock("plugin-c", "checkout", "command")
	mockC.Expect("checkout").Once() // even though plugin-b already ran checkout
	mockC.Expect("command").Once()

	mockD := setupMock("plugin-d", "command", "post-command")
	mockD.Expect("command").Once() // even though plugin-c already ran command
	mockD.Expect("post-command").Once()

	pluginsJSON, err := json.Marshal(testPlugins)
	if err != nil {
		t.Fatalf("json.Marshal(testPlugins) error = %v", err)
	}

	env := []string{
		"MY_CUSTOM_ENV=1",
		"BUILDKITE_PLUGINS=" + string(pluginsJSON),
	}

	tester.RunAndCheck(t, env...)
}

func TestPluginCloneRetried(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Not passing on windows, needs investigation")
	}

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

	hooks := map[string][]string{
		"environment": {
			"#!/usr/bin/env bash",
			"export LLAMAS_ROCK=absolutely",
		},
	}

	if runtime.GOOS == "windows" {
		hooks = map[string][]string{
			"environment.bat": {
				"@echo off",
				"set LLAMAS_ROCK=absolutely",
			},
		}
	}

	p := createTestPlugin(t, hooks)

	callCount := 0

	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("exec.LookPath(git) error = %v", err)
	}

	git := tester.MustMock(t, "git")

	git.Expect("clone", "-v", "--recursive", "--", p.Path, bintest.MatchAny()).Exactly(2).AndCallFunc(func(c *bintest.Call) {
		callCount++
		if callCount == 1 {
			c.Exit(1)
			return
		}
		c.Passthrough(realGit)
	})

	git.IgnoreUnexpectedInvocations()

	json, err := p.ToJSON()
	if err != nil {
		t.Fatalf("testPlugin.ToJSON() error = %v", err)
	}

	env := []string{
		"BUILDKITE_PLUGINS=" + json,
	}

	tester.RunAndCheck(t, env...)
}

// We want to support the situation where a user wants a plugin from a particular Git branch, e.g.,
// org/repo#my-dev-feature.  By default, if the Buildkite agent finds a plugin Git clone that
// matches the org, repo and ref, it will not try to pull or update it in any way, meaning that if
// the ref is a branch, and upstream has new commits, they will not get pulled in.  For that, we're
// introducing the BUILDKITE_PLUGINS_ALWAYS_CLONE_FRESH setting, which allows a user to force the
// agent to always make a fresh clone of any plugins.  This integration test and the one after test
// that a plugin modified upstream is treated as expected.  That is, by default, the updates won't
// take effect, but with BUILDKITE_PLUGINS_ALWAYS_CLONE_FRESH set, they will.
func TestModifiedPluginNoForcePull(t *testing.T) {
	t.Parallel()

	ctx := mainCtx

	tester, err := NewExecutorTester(ctx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

	// Let's set a fixed location for plugins, otherwise NewExecutorTester() gives us a random new
	// tempdir every time, which defeats our test.  Later we'll use this pluginsDir for the second
	// test run, too.
	pluginsDir, err := os.MkdirTemp("", "bootstrap-plugins")
	if err != nil {
		t.Fatalf(`os.MkdirTemp("", "bootstrap-plugins") error = %v`, err)
	}
	tester.PluginsDir = pluginsDir

	// There's a bit of machinery in replacePluginPathInEnv to modify only the
	// BUILDKITE_PLUGINS_PATH, leaving the rest of the environment variables NewExecutorTester()
	// gave us as-is.
	tester.Env = replacePluginPathInEnv(tester.Env, pluginsDir)

	// Create a test plugin that sets an environment variable.
	hooks := map[string][]string{
		"environment": {
			"#!/usr/bin/env bash",
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
	err = p.CreateBranch("something-fixed")
	assert.NilError(t, err)

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
			fmt.Fprintf(c.Stderr, "%v\n", err) //nolint:errcheck // test helper; write error is non-actionable
			c.Exit(1)
		} else {
			c.Exit(0)
		}
	})

	tester.RunAndCheck(t, env...)

	// Now, we want to "repeat" the test build, having modified the plugin's contents.
	tester2, err := NewExecutorTester(ctx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester2.Close() //nolint:errcheck // best-effort cleanup in test

	// Same modification of BUILDKITE_PLUGINS_PATH.
	tester2.PluginsDir = pluginsDir
	tester2.Env = replacePluginPathInEnv(tester2.Env, pluginsDir)

	hooks2 := map[string][]string{
		"environment": {
			"#!/usr/bin/env bash",
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
			fmt.Fprintf(c.Stderr, "%v\n", err) //nolint:errcheck // test helper; write error is non-actionable
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
func TestModifiedPluginWithForcePull(t *testing.T) {
	t.Parallel()

	ctx := mainCtx

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

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
			"#!/usr/bin/env bash",
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
	p.CreateBranch("something-fixed") //nolint:errcheck // test helper; branch creation error is non-critical
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
			fmt.Fprintf(c.Stderr, "%v\n", err) //nolint:errcheck // test helper; write error is non-actionable
			c.Exit(1)
		} else {
			c.Exit(0)
		}
	})

	tester.RunAndCheck(t, env...)

	tester2, err := NewExecutorTester(ctx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester2.Close() //nolint:errcheck // best-effort cleanup in test

	tester2.PluginsDir = pluginsDir
	tester2.Env = replacePluginPathInEnv(tester2.Env, pluginsDir)

	// Notice the all-important setting, BUILDKITE_PLUGINS_ALWAYS_CLONE_FRESH, being enabled.
	tester2.Env = append(tester2.Env, "BUILDKITE_PLUGINS_ALWAYS_CLONE_FRESH=true")

	hooks2 := map[string][]string{
		"environment": {
			"#!/usr/bin/env bash",
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
			fmt.Fprintf(c.Stderr, "%v\n", err) //nolint:errcheck // test helper; write error is non-actionable
			c.Exit(1)
		} else {
			c.Exit(0)
		}
	})

	tester2.RunAndCheck(t, env...)
}

type testPlugin struct {
	*gitRepository

	// What version of this mock plugin do we want?  Defaults to `git rev-parse HEAD`
	versionTag string
}

func createTestPlugin(t *testing.T, hooks map[string][]string) *testPlugin {
	t.Helper()

	repo, err := newGitRepository()
	if err != nil {
		t.Fatalf("newGitRepository() error = %v", err)
	}

	if err := os.MkdirAll(filepath.Join(repo.Path, "hooks"), 0o700); err != nil {
		t.Fatalf("os.MkdirAll(hooks, 0o700) = %v", err)
	}

	for hook, lines := range hooks {
		data := []byte(strings.Join(lines, "\n"))
		if err := os.WriteFile(filepath.Join(repo.Path, "hooks", hook), data, 0o600); err != nil {
			t.Fatalf("os.WriteFile(hooks/%s, data, 0o600) = %v", hook, err)
		}
	}

	if err := repo.Add("."); err != nil {
		t.Fatalf("repo.Add(.) = %v", err)
	}

	if err := repo.Commit("Initial commit of plugin hooks"); err != nil {
		t.Fatalf(`repo.Commit("Initial commit of plugin hooks") = %v`, err)
	}

	commitHash, err := repo.RevParse("HEAD")
	if err != nil {
		t.Fatalf(`repo.RevParse("HEAD") error = %v`, err)
	}
	return &testPlugin{repo, commitHash}
}

// modifyTestPlugin applies a change to a plugin's contents and makes a commit.  This is useful for
// testing BUILDKITE_PLUGINS_ALWAYS_CLONE_FRESH behaviour.
func modifyTestPlugin(t *testing.T, hooks map[string][]string, testPlugin *testPlugin) {
	t.Helper()

	repo := testPlugin.gitRepository

	for hook, lines := range hooks {
		data := []byte(strings.Join(lines, "\n"))
		if err := os.WriteFile(filepath.Join(repo.Path, "hooks", hook), data, 0o600); err != nil {
			t.Fatalf("os.WriteFile(hooks/%s, data, 0o600) = %v", hook, err)
		}
	}

	if err := repo.Add("."); err != nil {
		t.Fatalf("repo.Add(.) = %v", err)
	}

	if err := repo.Commit("Updating content of plugin"); err != nil {
		t.Fatalf(`repo.Commit("Updating content of plugin") = %v`, err)
	}
}

// ToJSON turns a single testPlugin into a single-item JSON
// array suitable for BUILDKITE_PLUGINS
func (tp *testPlugin) ToJSON() (string, error) {
	data, err := json.Marshal([]*testPlugin{tp})
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// MarshalJSON turns a single testPlugin into a JSON object.
// BUILDKITE_PLUGINS expects an array of these, so it would
// generally be used on a []testPlugin slice.
func (tp *testPlugin) MarshalJSON() ([]byte, error) {
	normalizedPath := strings.TrimPrefix(strings.ReplaceAll(tp.Path, "\\", "/"), "/")

	p := map[string]any{
		fmt.Sprintf("file:///%s#%s", normalizedPath, strings.TrimSpace(tp.versionTag)): map[string]string{
			"settings": "blah",
		},
	}
	b, err := json.Marshal(&p)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// replacePluginPathInEnv is useful for modifying the Env blob of a tester created with
// NewExecutorTester().  We need to do that because the tester relies on BUILDKITE_PLUGINS_PATH,
// not on the .PluginsDir field as one might expect.
func replacePluginPathInEnv(originalEnv []string, pluginsDir string) (newEnv []string) {
	newEnv = make([]string, 0, len(originalEnv))
	for _, e := range originalEnv {
		if strings.HasPrefix(e, "BUILDKITE_PLUGINS_PATH=") {
			newEnv = append(newEnv, "BUILDKITE_PLUGINS_PATH="+pluginsDir)
		} else {
			newEnv = append(newEnv, e)
		}
	}
	return newEnv
}
