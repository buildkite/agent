package integration

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
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
	defer tester.Close()

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
			fmt.Fprintf(c.Stderr, "%v\n", err)
			c.Exit(1)
		} else {
			c.Exit(0)
		}
	})

	tester.ExpectGlobalHook("command").Once().AndExitWith(0).AndCallFunc(func(c *bintest.Call) {
		if err := bintest.ExpectEnv(t, c.Env, "MY_CUSTOM_ENV=1", "LLAMAS_ROCK=absolutely"); err != nil {
			fmt.Fprintf(c.Stderr, "%v\n", err)
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
	defer tester.Close()

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
	defer tester.Close()

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
	defer tester.Close()

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
	defer tester.Close()

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
	defer tester.Close()

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
			fmt.Fprintf(c.Stderr, "%v\n", err)
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
	defer tester2.Close()

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
func TestModifiedPluginWithForcePull(t *testing.T) {
	t.Parallel()

	ctx := mainCtx

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
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
	p.CreateBranch("something-fixed")
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

	tester2, err := NewExecutorTester(ctx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester2.Close()

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
			fmt.Fprintf(c.Stderr, "%v\n", err)
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

func TestZipPluginFromLocalFile(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close()

	pluginMock := tester.MustMock(t, "my-plugin")

	hooks := map[string][]string{
		"environment": {
			"#!/usr/bin/env bash",
			"export ZIP_PLUGIN_LOADED=yes",
			pluginMock.Path + " testing",
		},
	}
	if runtime.GOOS == "windows" {
		hooks = map[string][]string{
			"environment.bat": {
				"@echo off",
				"set ZIP_PLUGIN_LOADED=yes",
				pluginMock.Path + " testing",
			},
		}
	}

	// Create a zip plugin
	zipPath := createTestZipPlugin(t, hooks)
	defer os.Remove(zipPath)

	// Create plugin JSON with zip+file:// URL
	pluginJSON := fmt.Sprintf(`[{"zip+file:///%s": {"config": "value"}}]`,
		strings.ReplaceAll(filepath.ToSlash(zipPath), "\\", "/"))

	env := []string{
		"BUILDKITE_PLUGINS=" + pluginJSON,
	}

	pluginMock.Expect("testing").Once().AndCallFunc(func(c *bintest.Call) {
		if err := bintest.ExpectEnv(t, c.Env, "ZIP_PLUGIN_LOADED=yes"); err != nil {
			fmt.Fprintf(c.Stderr, "%v\n", err)
			c.Exit(1)
		} else {
			c.Exit(0)
		}
	})

	tester.ExpectGlobalHook("command").Once().AndExitWith(0).AndCallFunc(func(c *bintest.Call) {
		if err := bintest.ExpectEnv(t, c.Env, "ZIP_PLUGIN_LOADED=yes"); err != nil {
			fmt.Fprintf(c.Stderr, "%v\n", err)
			c.Exit(1)
		} else {
			c.Exit(0)
		}
	})

	tester.RunAndCheck(t, env...)
}

func TestZipPluginMissingHooksDirectory(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close()

	// Create a zip file without hooks directory
	zipPath := createInvalidZipPlugin(t)
	defer os.Remove(zipPath)

	// Create plugin JSON with zip+file:// URL
	pluginJSON := fmt.Sprintf(`[{"zip+file:///%s": {}}]`,
		strings.ReplaceAll(filepath.ToSlash(zipPath), "\\", "/"))

	env := []string{
		"BUILDKITE_PLUGINS=" + pluginJSON,
	}

	// This should fail because the zip doesn't have a hooks directory
	err = tester.Run(t, env...)
	if err == nil {
		t.Fatalf("tester.Run() should have failed for zip without hooks directory")
	}

	// The job should fail (we got an error), which is what we expect
	// The actual error message will be in the shell output
}

// createTestZipPlugin creates a zip file containing a plugin with the given hooks
func createTestZipPlugin(t *testing.T, hooks map[string][]string) string {
	t.Helper()

	// Create temp directory for plugin content
	tempDir := t.TempDir()
	hooksDir := filepath.Join(tempDir, "hooks")
	if err := os.MkdirAll(hooksDir, 0o700); err != nil {
		t.Fatalf("os.MkdirAll(%q, 0o700) = %v", hooksDir, err)
	}

	// Write hook files
	for hook, lines := range hooks {
		data := []byte(strings.Join(lines, "\n"))
		hookPath := filepath.Join(hooksDir, hook)
		if err := os.WriteFile(hookPath, data, 0o700); err != nil {
			t.Fatalf("os.WriteFile(%q, data, 0o700) = %v", hookPath, err)
		}
	}

	// Create zip file
	zipFile, err := os.CreateTemp("", "test-plugin-*.zip")
	if err != nil {
		t.Fatalf("os.CreateTemp() error = %v", err)
	}
	zipPath := zipFile.Name()

	if err := createZipArchive(tempDir, zipPath); err != nil {
		t.Fatalf("createZipArchive() error = %v", err)
	}

	return zipPath
}

// createInvalidZipPlugin creates a zip file without a hooks directory
func createInvalidZipPlugin(t *testing.T) string {
	t.Helper()

	// Create temp directory without hooks
	tempDir := t.TempDir()

	// Write a dummy file
	dummyPath := filepath.Join(tempDir, "README.md")
	if err := os.WriteFile(dummyPath, []byte("Test plugin"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) = %v", dummyPath, err)
	}

	// Create zip file
	zipFile, err := os.CreateTemp("", "invalid-plugin-*.zip")
	if err != nil {
		t.Fatalf("os.CreateTemp() error = %v", err)
	}
	zipPath := zipFile.Name()

	if err := createZipArchive(tempDir, zipPath); err != nil {
		t.Fatalf("createZipArchive() error = %v", err)
	}

	return zipPath
}

// createZipArchive creates a zip archive from a source directory
func createZipArchive(sourceDir, zipPath string) error {
	// Create zip file
	zipFile, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer zipFile.Close()

	// Create zip writer
	zipWriter := zip.NewWriter(zipFile)
	defer zipWriter.Close()

	// Walk through source directory and add files to zip
	return filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Get relative path
		relPath, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}

		// Skip root directory
		if relPath == "." {
			return nil
		}

		// Create header
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}

		// Use forward slashes in zip file
		header.Name = filepath.ToSlash(relPath)

		// Set method to Deflate for better compression
		if !info.IsDir() {
			header.Method = zip.Deflate
		}

		// Create writer for file
		writer, err := zipWriter.CreateHeader(header)
		if err != nil {
			return err
		}

		// If it's a directory, we're done
		if info.IsDir() {
			return nil
		}

		// Copy file content
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		_, err = io.Copy(writer, file)
		return err
	})
}
