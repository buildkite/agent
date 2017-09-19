package integration

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lox/bintest"
	"github.com/lox/bintest/proxy"
)

func TestRunningPlugins(t *testing.T) {
	tester, err := NewBootstrapTester()
	if err != nil {
		t.Fatal(err)
	}
	defer tester.Close()

	pluginMock := tester.MustMock(t, "my-plugin")

	p := createTestPlugin(t, map[string][]string{
		"environment": []string{
			"#!/bin/bash",
			"export LLAMAS_ROCK=absolutely",
			pluginMock.Path + " testing",
		},
	})

	json, err := p.ToJSON()
	if err != nil {
		t.Fatal(err)
	}

	env := []string{
		`MY_CUSTOM_ENV=1`,
		`BUILDKITE_PLUGINS=` + json,
	}

	pluginMock.Expect("testing").Once().AndCallFunc(func(c *proxy.Call) {
		if err := bintest.ExpectEnv(t, c.Env, `MY_CUSTOM_ENV=1`, `LLAMAS_ROCK=absolutely`); err != nil {
			fmt.Fprintf(c.Stderr, "%v\n", err)
			c.Exit(1)
		}
		c.Exit(0)
	})

	tester.ExpectGlobalHook("command").Once().AndExitWith(0).AndCallFunc(func(c *proxy.Call) {
		if err := bintest.ExpectEnv(t, c.Env, `MY_CUSTOM_ENV=1`, `LLAMAS_ROCK=absolutely`); err != nil {
			fmt.Fprintf(c.Stderr, "%v\n", err)
			c.Exit(1)
		}
		c.Exit(0)
	})

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

func (tp *testPlugin) ToJSON() (string, error) {
	commitHash, err := tp.RevParse("HEAD")
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(`[{"%s#%s":{"setting":"blah"}}]`, tp.Path, strings.TrimSpace(commitHash)), nil
}
