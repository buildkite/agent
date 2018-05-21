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

	"github.com/buildkite/agent/bootstrap/shell"
	"github.com/buildkite/bintest"
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

func (tp *testPlugin) ToJSON() (string, error) {
	commitHash, err := tp.RevParse("HEAD")
	if err != nil {
		return "", err
	}
	normalizedPath := strings.TrimPrefix(strings.Replace(tp.Path, "\\", "/", -1), "/")

	var p = []interface{}{map[string]interface{}{
		fmt.Sprintf(`file:///%s#%s`, normalizedPath, strings.TrimSpace(commitHash)): map[string]string{
			"settings": "blah",
		},
	}}
	b, err := json.Marshal(&p)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
