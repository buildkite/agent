package integration

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/bootstrap/shell"
	"github.com/buildkite/bintest/v3"
)

func TestRunningPlugins(t *testing.T) {
	t.Parallel()

	tester, err := NewBootstrapTester()
	if err != nil {
		t.Fatal(err)
	}
	defer tester.Close()

	pluginMock := tester.MustMock(t, "my-plugin")

	var p *testPlugin

	if runtime.GOOS == "windows" {
		p = createTestPlugin(t, map[string][]string{
			"environment.bat": []string{
				"@echo off",
				"set LLAMAS_ROCK=absolutely",
				pluginMock.Path + " testing",
			},
		})
	} else {
		p = createTestPlugin(t, map[string][]string{
			"environment": []string{
				"#!/bin/bash",
				"export LLAMAS_ROCK=absolutely",
				pluginMock.Path + " testing",
			},
		})
	}

	json, err := p.ToJSON()
	if err != nil {
		t.Fatal(err)
	}

	env := []string{
		`MY_CUSTOM_ENV=1`,
		`BUILDKITE_PLUGINS=` + json,
	}

	pluginMock.Expect("testing").Once().AndCallFunc(func(c *bintest.Call) {
		if err := bintest.ExpectEnv(t, c.Env, `MY_CUSTOM_ENV=1`, `LLAMAS_ROCK=absolutely`); err != nil {
			fmt.Fprintf(c.Stderr, "%v\n", err)
			c.Exit(1)
		}
		c.Exit(0)
	})

	tester.ExpectGlobalHook("command").Once().AndExitWith(0).AndCallFunc(func(c *bintest.Call) {
		if err := bintest.ExpectEnv(t, c.Env, `MY_CUSTOM_ENV=1`, `LLAMAS_ROCK=absolutely`); err != nil {
			fmt.Fprintf(c.Stderr, "%v\n", err)
			c.Exit(1)
		}
		c.Exit(0)
	})

	tester.RunAndCheck(t, env...)
}

func TestExitCodesPropagateOutFromPlugins(t *testing.T) {
	t.Parallel()

	tester, err := NewBootstrapTester()
	if err != nil {
		t.Fatal(err)
	}
	defer tester.Close()

	var p *testPlugin

	if runtime.GOOS == "windows" {
		p = createTestPlugin(t, map[string][]string{
			"environment.bat": []string{
				"@echo off",
				"exit 5",
			},
		})
	} else {
		p = createTestPlugin(t, map[string][]string{
			"environment": []string{
				"#!/bin/bash",
				"exit 5",
			},
		})
	}

	json, err := p.ToJSON()
	if err != nil {
		t.Fatal(err)
	}

	env := []string{
		`BUILDKITE_PLUGINS=` + json,
	}

	err = tester.Run(t, env...)
	if err == nil {
		t.Fatal("Expected the bootstrap to fail")
	}

	exitCode := shell.GetExitCode(err)

	if exitCode != 5 {
		t.Fatalf("Expected an exit code of %d, got %d", 5, exitCode)
	}

	tester.CheckMocks(t)
}

func TestMalformedPluginNamesDontCrashBootstrap(t *testing.T) {
	t.Parallel()

	tester, err := NewBootstrapTester()
	if err != nil {
		t.Fatal(err)
	}
	defer tester.Close()

	env := []string{
		`BUILDKITE_PLUGINS=["sdgmdgn.@$!sdf,asdf#llamas"]`,
	}

	if err = tester.Run(t, env...); err == nil {
		t.Fatal("Expected the bootstrap to fail")
	}

	tester.CheckMocks(t)
}

// A job may have multiple plugins that provide multiple hooks of a given type.
// For a while (late 2019 / early 2020) we disallowed duplicate checkout and
// command hooks from plugins; only the first would execute.  We since decided
// to roll that back and permit e.g. multiple checkout plugin hooks.
func TestOverlappingPluginHooks(t *testing.T) {
	t.Parallel()

	tester, err := NewBootstrapTester()
	if err != nil {
		t.Fatal(err)
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
					"#!/bin/bash",
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
		t.Fatal(err)
	}

	env := []string{
		`MY_CUSTOM_ENV=1`,
		`BUILDKITE_PLUGINS=` + string(pluginsJSON),
	}

	tester.RunAndCheck(t, env...)
}

type testPlugin struct {
	*gitRepository
}

func createTestPlugin(t *testing.T, hooks map[string][]string) *testPlugin {
	repo, err := newGitRepository()
	if err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(repo.Path, "hooks"), 0700); err != nil {
		t.Fatal(err)
	}

	for hook, lines := range hooks {
		data := []byte(strings.Join(lines, "\n"))
		if err := ioutil.WriteFile(filepath.Join(repo.Path, "hooks", hook), data, 0600); err != nil {
			t.Fatal(err)
		}
	}

	if err = repo.Add("."); err != nil {
		t.Fatal(err)
	}

	if err = repo.Commit("Initial commit of plugin hooks"); err != nil {
		t.Fatal(err)
	}

	return &testPlugin{repo}
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
	commitHash, err := tp.RevParse("HEAD")
	if err != nil {
		return nil, err
	}
	normalizedPath := strings.TrimPrefix(strings.Replace(tp.Path, "\\", "/", -1), "/")

	p := map[string]interface{}{
		fmt.Sprintf(`file:///%s#%s`, normalizedPath, strings.TrimSpace(commitHash)): map[string]string{
			"settings": "blah",
		},
	}
	b, err := json.Marshal(&p)
	if err != nil {
		return nil, err
	}
	return b, nil
}
