package integration

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/buildkite/agent/v3/experiments"
	"github.com/buildkite/bintest/v3"
)

// Example commit info:
//
// commit 65e2f46931cf9fe1ba9e445d92d213cfa3be5312
// abbrev-commit 65e2f46931
// Author:     Example Human <legit@example.com>
//
//	hello world
var commitPattern = bintest.MatchPattern(`(?ms)\Acommit [0-9a-f]+\nabbrev-commit [0-9a-f]+\n.*^Author:`)

// We expect this arg multiple times, just define it once.
var gitShowFormatArg = "--format=commit %H%nabbrev-commit %h%nAuthor: %an <%ae>%n%n%w(0,4,4)%B"

// Enable an experiment, returning a function to restore the previous state.
// Usage: defer experimentWithUndo("foo")()
func experimentWithUndo(name string) func() {
	prev := experiments.IsEnabled(name)
	experiments.Enable(name)
	return func() {
		if !prev {
			experiments.Disable(name)
		}
	}
}

func TestCheckingOutGitHubPullRequestsWithGitMirrorsExperiment(t *testing.T) {
	// t.Parallel() cannot be used with experiments.Enable()
	defer experimentWithUndo(experiments.GitMirrors)()

	tester, err := NewExecutorTester()
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close()

	env := []string{
		"BUILDKITE_GIT_CLONE_MIRROR_FLAGS=--bare",
		"BUILDKITE_PULL_REQUEST=123",
		"BUILDKITE_PIPELINE_PROVIDER=github",
	}

	// Actually execute git commands, but with expectations
	git := tester.
		MustMock(t, "git").
		PassthroughToLocalCommand()

	// But assert which ones are called
	git.ExpectAll([][]any{
		{"clone", "--mirror", "--bare", "--", tester.Repo.Path, matchSubDir(tester.GitMirrorsDir)},
		{"clone", "-v", "--reference", matchSubDir(tester.GitMirrorsDir), "--", tester.Repo.Path, "."},
		{"clean", "-ffxdq"},
		{"fetch", "--", "origin", "refs/pull/123/head"},
		{"rev-parse", "FETCH_HEAD"},
		{"checkout", "-f", "FETCH_HEAD"},
		{"clean", "-ffxdq"},
		{"--no-pager", "show", "HEAD", "-s", "--no-color", gitShowFormatArg},
	})

	// Mock out the meta-data calls to the agent after checkout
	agent := tester.MockAgent(t)
	agent.Expect("meta-data", "exists", "buildkite:git:commit").AndExitWith(1)
	agent.Expect("meta-data", "set", "buildkite:git:commit").WithStdin(commitPattern)

	tester.RunAndCheck(t, env...)
}

func TestWithResolvingCommitExperiment(t *testing.T) {
	// t.Parallel() cannot be used with experiments.Enable()
	defer experimentWithUndo(experiments.ResolveCommitAfterCheckout)()

	tester, err := NewExecutorTester()
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close()

	env := []string{
		"BUILDKITE_GIT_CLONE_FLAGS=-v",
		"BUILDKITE_GIT_CLONE_MIRROR_FLAGS=--bare",
		"BUILDKITE_GIT_CLEAN_FLAGS=-fdq",
		"BUILDKITE_GIT_FETCH_FLAGS=-v",
	}

	// Actually execute git commands, but with expectations
	git := tester.
		MustMock(t, "git").
		PassthroughToLocalCommand()

	// But assert which ones are called
	if experiments.IsEnabled(experiments.GitMirrors) {
		git.ExpectAll([][]any{
			{"clone", "--mirror", "--bare", "--", tester.Repo.Path, matchSubDir(tester.GitMirrorsDir)},
			{"clone", "-v", "--reference", matchSubDir(tester.GitMirrorsDir), "--", tester.Repo.Path, "."},
			{"clean", "-fdq"},
			{"fetch", "-v", "--", "origin", "main"},
			{"checkout", "-f", "FETCH_HEAD"},
			{"clean", "-fdq"},
			{"--no-pager", "show", "HEAD", "-s", "--no-color", gitShowFormatArg},
			{"rev-parse", "HEAD"},
		})
	} else {
		git.ExpectAll([][]any{
			{"clone", "-v", "--", tester.Repo.Path, "."},
			{"clean", "-fdq"},
			{"fetch", "-v", "--", "origin", "main"},
			{"checkout", "-f", "FETCH_HEAD"},
			{"clean", "-fdq"},
			{"--no-pager", "show", "HEAD", "-s", "--no-color", gitShowFormatArg},
			{"rev-parse", "HEAD"},
		})
	}

	// Mock out the meta-data calls to the agent after checkout
	agent := tester.MockAgent(t)
	agent.Expect("meta-data", "exists", "buildkite:git:commit").AndExitWith(1)
	agent.Expect("meta-data", "set", "buildkite:git:commit").WithStdin(commitPattern)

	tester.RunAndCheck(t, env...)
}

func TestCheckingOutLocalGitProject(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester()
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close()

	env := []string{
		"BUILDKITE_GIT_CLONE_FLAGS=-v",
		"BUILDKITE_GIT_CLONE_MIRROR_FLAGS=--config pack.threads=35",
		"BUILDKITE_GIT_CLEAN_FLAGS=-fdq",
		"BUILDKITE_GIT_FETCH_FLAGS=-v",
	}

	// Actually execute git commands, but with expectations
	git := tester.
		MustMock(t, "git").
		PassthroughToLocalCommand()

	// But assert which ones are called
	if experiments.IsEnabled(experiments.GitMirrors) {
		git.ExpectAll([][]any{
			{"clone", "--mirror", "--config", "pack.threads=35", "--", tester.Repo.Path, matchSubDir(tester.GitMirrorsDir)},
			{"clone", "-v", "--reference", matchSubDir(tester.GitMirrorsDir), "--", tester.Repo.Path, "."},
			{"clean", "-fdq"},
			{"fetch", "-v", "--", "origin", "main"},
			{"checkout", "-f", "FETCH_HEAD"},
			{"clean", "-fdq"},
			{"--no-pager", "show", "HEAD", "-s", "--no-color", gitShowFormatArg},
		})
	} else {
		git.ExpectAll([][]any{
			{"clone", "-v", "--", tester.Repo.Path, "."},
			{"clean", "-fdq"},
			{"fetch", "-v", "--", "origin", "main"},
			{"checkout", "-f", "FETCH_HEAD"},
			{"clean", "-fdq"},
			{"--no-pager", "show", "HEAD", "-s", "--no-color", gitShowFormatArg},
		})
	}

	// Mock out the meta-data calls to the agent after checkout
	agent := tester.MockAgent(t)
	agent.Expect("meta-data", "exists", "buildkite:git:commit").AndExitWith(1)
	agent.Expect("meta-data", "set", "buildkite:git:commit").WithStdin(commitPattern)

	tester.RunAndCheck(t, env...)
}

func TestCheckingOutLocalGitProjectWithSubmodules(t *testing.T) {
	t.Parallel()

	// Git for windows seems to struggle with local submodules in the temp dir
	if runtime.GOOS == "windows" {
		t.Skip()
	}

	tester, err := NewExecutorTester()
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close()

	submoduleRepo, err := createTestGitRespository()
	if err != nil {
		t.Fatalf("createTestGitRepository() error = %v", err)
	}
	defer submoduleRepo.Close()

	out, err := tester.Repo.Execute("-c", "protocol.file.allow=always", "submodule", "add", submoduleRepo.Path)
	if err != nil {
		t.Fatalf("tester.Repo.Execute(submodule, add, %q) error = %v\nout = %s", submoduleRepo.Path, err, out)
	}

	out, err = tester.Repo.Execute("commit", "-am", "Add example submodule")
	if err != nil {
		t.Fatalf(`tester.Repo.Execute(commit, -am, "Add example submodule") error = %v\nout = %s`, err, out)
	}

	env := []string{
		"BUILDKITE_GIT_CLONE_FLAGS=-v",
		"BUILDKITE_GIT_CLEAN_FLAGS=-fdq",
		"BUILDKITE_GIT_FETCH_FLAGS=-v",
		"BUILDKITE_GIT_SUBMODULE_CLONE_CONFIG=protocol.file.allow=always",
	}

	// Actually execute git commands, but with expectations
	git := tester.
		MustMock(t, "git").
		PassthroughToLocalCommand()

	// But assert which ones are called
	if experiments.IsEnabled(experiments.GitMirrors) {
		git.ExpectAll([][]any{
			{"clone", "--mirror", "-v", "--", tester.Repo.Path, matchSubDir(tester.GitMirrorsDir)},
			{"clone", "--mirror", "-v", "--", submoduleRepo.Path, matchSubDir(tester.GitMirrorsDir)},
			{"clone", "-v", "--reference", matchSubDir(tester.GitMirrorsDir), "--", tester.Repo.Path, "."},
			{"clean", "-fdq"},
			{"submodule", "foreach", "--recursive", "git clean -fdq"},
			{"fetch", "-v", "--", "origin", "main"},
			{"checkout", "-f", "FETCH_HEAD"},
			{"submodule", "sync", "--recursive"},
			{"config", "--file", ".gitmodules", "--null", "--get-regexp", "submodule\\..+\\.url"},
			{"-c", "protocol.file.allow=always", "submodule", "update", "--init", "--recursive", "--force", "--reference", submoduleRepo.Path},
			{"submodule", "foreach", "--recursive", "git reset --hard"},
			{"clean", "-fdq"},
			{"submodule", "foreach", "--recursive", "git clean -fdq"},
			{"--no-pager", "show", "HEAD", "-s", "--no-color", gitShowFormatArg},
		})
	} else {
		git.ExpectAll([][]any{
			{"clone", "-v", "--", tester.Repo.Path, "."},
			{"clean", "-fdq"},
			{"submodule", "foreach", "--recursive", "git clean -fdq"},
			{"fetch", "-v", "--", "origin", "main"},
			{"checkout", "-f", "FETCH_HEAD"},
			{"submodule", "sync", "--recursive"},
			{"config", "--file", ".gitmodules", "--null", "--get-regexp", "submodule\\..+\\.url"},
			{"-c", "protocol.file.allow=always", "submodule", "update", "--init", "--recursive", "--force"},
			{"submodule", "foreach", "--recursive", "git reset --hard"},
			{"clean", "-fdq"},
			{"submodule", "foreach", "--recursive", "git clean -fdq"},
			{"--no-pager", "show", "HEAD", "-s", "--no-color", gitShowFormatArg},
		})
	}

	// Mock out the meta-data calls to the agent after checkout
	agent := tester.MockAgent(t)
	agent.Expect("meta-data", "exists", "buildkite:git:commit").AndExitWith(1)
	agent.Expect("meta-data", "set", "buildkite:git:commit").WithStdin(commitPattern)

	tester.RunAndCheck(t, env...)
}

func TestCheckingOutLocalGitProjectWithSubmodulesDisabled(t *testing.T) {
	t.Parallel()

	// Git for windows seems to struggle with local submodules in the temp dir
	if runtime.GOOS == "windows" {
		t.Skip()
	}

	tester, err := NewExecutorTester()
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close()

	submoduleRepo, err := createTestGitRespository()
	if err != nil {
		t.Fatalf("createTestGitRespository() error = %v", err)
	}
	defer submoduleRepo.Close()

	out, err := tester.Repo.Execute("-c", "protocol.file.allow=always", "submodule", "add", submoduleRepo.Path)
	if err != nil {
		t.Fatalf("tester.Repo.Execute(submodule, add, %q) error = %v\nout = %s", submoduleRepo.Path, err, out)
	}

	out, err = tester.Repo.Execute("commit", "-am", "Add example submodule")
	if err != nil {
		t.Fatalf(`tester.Repo.Execute(commit, -am, "Add example submodule") error = %v\nout = %s`, err, out)
	}

	env := []string{
		"BUILDKITE_GIT_CLONE_FLAGS=-v",
		"BUILDKITE_GIT_CLEAN_FLAGS=-fdq",
		"BUILDKITE_GIT_FETCH_FLAGS=-v",
		"BUILDKITE_GIT_SUBMODULES=false",
	}

	// Actually execute git commands, but with expectations
	git := tester.
		MustMock(t, "git").
		PassthroughToLocalCommand()

	// But assert which ones are called
	if experiments.IsEnabled(experiments.GitMirrors) {
		git.ExpectAll([][]any{
			{"clone", "--mirror", "-v", "--", tester.Repo.Path, matchSubDir(tester.GitMirrorsDir)},
			{"clone", "-v", "--reference", matchSubDir(tester.GitMirrorsDir), "--", tester.Repo.Path, "."},
			{"clean", "-fdq"},
			{"submodule", "foreach", "--recursive", "git clean -fdq"},
			{"fetch", "-v", "--", "origin", "main"},
			{"checkout", "-f", "FETCH_HEAD"},
			{"clean", "-fdq"},
			{"--no-pager", "show", "HEAD", "-s", "--no-color", gitShowFormatArg},
		})
	} else {
		git.ExpectAll([][]any{
			{"clone", "-v", "--", tester.Repo.Path, "."},
			{"clean", "-fdq"},
			{"submodule", "foreach", "--recursive", "git clean -fdq"},
			{"fetch", "-v", "--", "origin", "main"},
			{"checkout", "-f", "FETCH_HEAD"},
			{"clean", "-fdq"},
			{"--no-pager", "show", "HEAD", "-s", "--no-color", gitShowFormatArg},
		})
	}

	// Mock out the meta-data calls to the agent after checkout
	agent := tester.MockAgent(t)
	agent.Expect("meta-data", "exists", "buildkite:git:commit").AndExitWith(1)
	agent.Expect("meta-data", "set", "buildkite:git:commit").WithStdin(commitPattern)

	tester.RunAndCheck(t, env...)
}

func TestCheckingOutShallowCloneOfLocalGitProject(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester()
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close()

	env := []string{
		"BUILDKITE_GIT_CLONE_FLAGS=--depth=1",
		"BUILDKITE_GIT_CLONE_MIRROR_FLAGS=--bare",
		"BUILDKITE_GIT_CLEAN_FLAGS=-fdq",
		"BUILDKITE_GIT_FETCH_FLAGS=--depth=1",
	}

	// Actually execute git commands, but with expectations
	git := tester.
		MustMock(t, "git").
		PassthroughToLocalCommand()

	// But assert which ones are called
	if experiments.IsEnabled(experiments.GitMirrors) {
		git.ExpectAll([][]any{
			{"clone", "--mirror", "--bare", "--", tester.Repo.Path, matchSubDir(tester.GitMirrorsDir)},
			{"clone", "--depth=1", "--reference", matchSubDir(tester.GitMirrorsDir), "--", tester.Repo.Path, "."},
			{"clean", "-fdq"},
			{"fetch", "--depth=1", "--", "origin", "main"},
			{"checkout", "-f", "FETCH_HEAD"},
			{"clean", "-fdq"},
			{"--no-pager", "show", "HEAD", "-s", "--no-color", gitShowFormatArg},
		})
	} else {
		git.ExpectAll([][]any{
			{"clone", "--depth=1", "--", tester.Repo.Path, "."},
			{"clean", "-fdq"},
			{"fetch", "--depth=1", "--", "origin", "main"},
			{"checkout", "-f", "FETCH_HEAD"},
			{"clean", "-fdq"},
			{"--no-pager", "show", "HEAD", "-s", "--no-color", gitShowFormatArg},
		})
	}

	// Mock out the meta-data calls to the agent after checkout
	agent := tester.MockAgent(t)
	agent.Expect("meta-data", "exists", "buildkite:git:commit").AndExitWith(1)
	agent.Expect("meta-data", "set", "buildkite:git:commit").WithStdin(commitPattern)

	tester.RunAndCheck(t, env...)
}

func TestCheckingOutSetsCorrectGitMetadataAndSendsItToBuildkite(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester()
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close()

	agent := tester.MockAgent(t)
	agent.Expect("meta-data", "exists", "buildkite:git:commit").AndExitWith(1)
	agent.Expect("meta-data", "set", "buildkite:git:commit").WithStdin(commitPattern)

	tester.RunAndCheck(t)
}

func TestCheckingOutWithSSHKeyscan(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester()
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close()

	tester.MustMock(t, "ssh-keyscan").
		Expect("github.com").
		AndWriteToStdout("github.com ssh-rsa xxx=").
		AndExitWith(0)

	git := tester.MustMock(t, "git")
	git.IgnoreUnexpectedInvocations()

	if experiments.IsEnabled(experiments.GitMirrors) {
		git.Expect("clone", "--mirror", "-v", "--", "git@github.com:buildkite/agent.git", bintest.MatchAny()).
			AndExitWith(0)
	} else {
		git.Expect("clone", "-v", "--", "git@github.com:buildkite/agent.git", ".").
			AndExitWith(0)
	}

	env := []string{
		"BUILDKITE_REPO=git@github.com:buildkite/agent.git",
		"BUILDKITE_SSH_KEYSCAN=true",
	}

	tester.RunAndCheck(t, env...)
}

func TestCheckingOutWithoutSSHKeyscan(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester()
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close()

	tester.MustMock(t, "ssh-keyscan").
		Expect("github.com").
		NotCalled()

	env := []string{
		"BUILDKITE_REPO=https://github.com/buildkite/bash-example.git",
		"BUILDKITE_SSH_KEYSCAN=false",
	}

	tester.RunAndCheck(t, env...)
}

func TestCheckingOutWithSSHKeyscanAndUnscannableRepo(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester()
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close()

	tester.MustMock(t, "ssh-keyscan").
		Expect("github.com").
		NotCalled()

	git := tester.MustMock(t, "git")
	git.IgnoreUnexpectedInvocations()

	if experiments.IsEnabled(experiments.GitMirrors) {
		git.Expect("clone", "--mirror", "-v", "--", "https://github.com/buildkite/bash-example.git", bintest.MatchAny()).
			AndExitWith(0)
	} else {
		git.Expect("clone", "-v", "--", "https://github.com/buildkite/bash-example.git", ".").
			AndExitWith(0)
	}

	env := []string{
		"BUILDKITE_REPO=https://github.com/buildkite/bash-example.git",
		"BUILDKITE_SSH_KEYSCAN=true",
	}

	tester.RunAndCheck(t, env...)
}

func TestCleaningAnExistingCheckout(t *testing.T) {
	t.Skip("TODO: Figure out what this test is actually supposed to be testing")

	t.Parallel()

	tester, err := NewExecutorTester()
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close()

	// Create an existing checkout
	out, err := tester.Repo.Execute("clone", "-v", "--", tester.Repo.Path, tester.CheckoutDir())
	if err != nil {
		t.Fatalf(`tester.Repo.Execute(clone, -v, --, %q, %q) error = %v\nout = %s`, tester.Repo.Path, tester.CheckoutDir(), err, out)
	}
	testpath := filepath.Join(tester.CheckoutDir(), "test.txt")
	if err := os.WriteFile(testpath, []byte("llamas"), 0700); err != nil {
		t.Fatalf("os.WriteFile(test.txt, llamas, 0700) = %v", err)
	}

	// Mock out the meta-data calls to the agent after checkout
	agent := tester.MockAgent(t)
	agent.
		Expect("meta-data", "exists", "buildkite:git:commit").
		AndExitWith(0)

	tester.RunAndCheck(t)

	// This used to check if os.IsExist(err) == true.
	// Unfortunately, os.IsExist(err) is not the same as !os.IsNotExist(err)
	// (https://go.dev/play/p/j8z6jsF5qJs) and the code under test isn't
	// removing the file.
	if _, err := os.Stat(testpath); !os.IsNotExist(err) {
		t.Errorf("os.Stat(test.txt) error = nil, want no such file or directory")
	}
}

func TestForcingACleanCheckout(t *testing.T) {
	tester, err := NewExecutorTester()
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close()

	// Mock out the meta-data calls to the agent after checkout
	agent := tester.MockAgent(t)
	agent.
		Expect("meta-data", "exists", "buildkite:git:commit").
		AndExitWith(0)

	tester.RunAndCheck(t, "BUILDKITE_CLEAN_CHECKOUT=true")

	if !strings.Contains(tester.Output, "Cleaning pipeline checkout") {
		t.Fatal(`tester.Output does not contain "Cleaning pipeline checkout"`)
	}
}

func TestCheckoutOnAnExistingRepositoryWithoutAGitFolder(t *testing.T) {
	tester, err := NewExecutorTester()
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close()

	// Create an existing checkout
	out, err := tester.Repo.Execute("clone", "-v", "--", tester.Repo.Path, tester.CheckoutDir())
	if err != nil {
		t.Fatalf(`tester.Repo.Execute(clone, -v, --, %q, %q) error = %v\nout = %s`, tester.Repo.Path, tester.CheckoutDir(), err, out)
	}

	if err := os.RemoveAll(filepath.Join(tester.CheckoutDir(), ".git", "refs")); err != nil {
		t.Fatalf("os.RemoveAll(.git/refs) = %v", err)
	}

	agent := tester.MockAgent(t)
	agent.
		Expect("meta-data", "exists", "buildkite:git:commit").
		AndExitWith(0)

	tester.RunAndCheck(t)
}

func TestCheckoutRetriesOnCleanFailure(t *testing.T) {
	tester, err := NewExecutorTester()
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close()

	var cleanCounter int32

	// Mock out all git commands, passing them through to the real thing unless it's a checkout
	git := tester.MustMock(t, "git").PassthroughToLocalCommand().Before(func(i bintest.Invocation) error {
		if i.Args[0] == "clean" {
			c := atomic.AddInt32(&cleanCounter, 1)

			// NB: clean gets run twice per checkout
			if c == 1 {
				return errors.New("Sunspots have caused git clean to fail")
			}
		}
		return nil
	})

	git.Expect().AtLeastOnce().WithAnyArguments()
	tester.RunAndCheck(t)
}

func TestCheckoutRetriesOnCloneFailure(t *testing.T) {
	tester, err := NewExecutorTester()
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close()

	var cloneCounter int32

	// Mock out all git commands, passing them through to the real thing unless it's a checkout
	git := tester.MustMock(t, "git").PassthroughToLocalCommand().Before(func(i bintest.Invocation) error {
		if i.Args[0] == "clone" {
			c := atomic.AddInt32(&cloneCounter, 1)
			if c == 1 {
				return errors.New("Sunspots have caused git clone to fail")
			}
		}
		return nil
	})

	git.Expect().AtLeastOnce().WithAnyArguments()
	tester.RunAndCheck(t)
}

func TestCheckoutDoesNotRetryOnHookFailure(t *testing.T) {
	tester, err := NewExecutorTester()
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close()

	var checkoutCounter int32

	tester.ExpectGlobalHook("checkout").Once().AndCallFunc(func(c *bintest.Call) {
		counter := atomic.AddInt32(&checkoutCounter, 1)
		fmt.Fprintf(c.Stdout, "Checkout invocation %d\n", counter)
		if counter == 1 {
			fmt.Fprintf(c.Stdout, "Sunspots have caused checkout to fail\n")
			c.Exit(1)
		} else {
			c.Exit(0)
		}
	})

	if err := tester.Run(t); err == nil {
		t.Fatalf("tester.Run(t) = %v, want non-nil error", err)
	}

	tester.CheckMocks(t)
}

func TestRepositorylessCheckout(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Not supported on windows")
	}

	tester, err := NewExecutorTester()
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close()

	var script = []string{
		"#!/bin/bash",
		"export BUILDKITE_REPO=",
	}

	if err := os.WriteFile(filepath.Join(tester.HooksDir, "environment"), []byte(strings.Join(script, "\n")), 0700); err != nil {
		t.Fatalf("os.WriteFile(environment, script, 0700) = %v", err)
	}

	tester.MustMock(t, "git").Expect().NotCalled()

	tester.ExpectGlobalHook("pre-checkout").Once()
	tester.ExpectGlobalHook("post-checkout").Once()
	tester.ExpectGlobalHook("pre-command").Once()
	tester.ExpectGlobalHook("post-command").Once()
	tester.ExpectGlobalHook("pre-exit").Once()

	tester.RunAndCheck(t)
}

func TestGitMirrorEnv(t *testing.T) {
	// t.Parallel() cannot test experiment flags in parallel
	defer experimentWithUndo(experiments.GitMirrors)()

	tester, err := NewExecutorTester()
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close()

	// assert the correct BUILDKITE_REPO_MIRROR _after_ the executor has run
	gitMirrorPath := ""
	tester.ExpectGlobalHook("pre-command").Once().AndCallFunc(func(c *bintest.Call) {
		gitMirrorPath = c.GetEnv("BUILDKITE_REPO_MIRROR")
		c.Exit(0)
	})

	env := []string{
		"BUILDKITE_GIT_CLONE_FLAGS=-v",
		"BUILDKITE_GIT_CLONE_MIRROR_FLAGS=--bare",
		"BUILDKITE_GIT_CLEAN_FLAGS=-fdq",
		"BUILDKITE_GIT_FETCH_FLAGS=-v",
	}

	// Actually execute git commands, but with expectations
	git := tester.
		MustMock(t, "git").
		PassthroughToLocalCommand()

	// But assert which ones are called
	if experiments.IsEnabled(experiments.GitMirrors) {
		git.ExpectAll([][]any{
			{"clone", "--mirror", "--bare", "--", tester.Repo.Path, matchSubDir(tester.GitMirrorsDir)},
			{"clone", "-v", "--reference", matchSubDir(tester.GitMirrorsDir), "--", tester.Repo.Path, "."},
			{"clean", "-fdq"},
			{"fetch", "-v", "--", "origin", "main"},
			{"checkout", "-f", "FETCH_HEAD"},
			{"clean", "-fdq"},
			{"--no-pager", "show", "HEAD", "-s", "--no-color", gitShowFormatArg},
		})
	} else {
		git.ExpectAll([][]any{
			{"clone", "-v", "--", tester.Repo.Path, "."},
			{"clean", "-fdq"},
			{"fetch", "-v", "--", "origin", "main"},
			{"checkout", "-f", "FETCH_HEAD"},
			{"clean", "-fdq"},
			{"--no-pager", "show", "HEAD", "-s", "--no-color", gitShowFormatArg},
		})
	}

	// Mock out the meta-data calls to the agent after checkout
	agent := tester.MockAgent(t)
	agent.Expect("meta-data", "exists", "buildkite:git:commit").AndExitWith(1)
	agent.Expect("meta-data", "set", "buildkite:git:commit").WithStdin(commitPattern)

	tester.RunAndCheck(t, env...)

	if !strings.HasPrefix(gitMirrorPath, tester.GitMirrorsDir) {
		t.Errorf("gitMirrorPath = %q, want prefix %q", gitMirrorPath, tester.GitMirrorsDir)
	}
}

type subDirMatcher struct {
	dir string
}

func (mf subDirMatcher) Match(s string) (bool, string) {
	if filepath.Dir(s) == mf.dir {
		return true, ""
	}
	return false, fmt.Sprintf("%s wasn't a sub directory of %s", s, mf.dir)
}

func (mf subDirMatcher) String() string {
	return fmt.Sprintf("subDirMatcher(%q)", mf.dir)
}

func matchSubDir(dir string) bintest.Matcher {
	return subDirMatcher{dir: dir}
}
