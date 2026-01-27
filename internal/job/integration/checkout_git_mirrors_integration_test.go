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

	"github.com/buildkite/agent/v3/internal/experiments"
	"github.com/buildkite/agent/v3/internal/job"
	"github.com/buildkite/bintest/v3"
)

func TestCheckingOutGitHubPullRequests_WithGitMirrors(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close()

	if err := tester.EnableGitMirrors(); err != nil {
		t.Fatalf("EnableGitMirrors() error = %v", err)
	}

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
		{"--no-pager", "log", "-1", "HEAD", "-s", "--no-color", gitShowFormatArg},
	})

	// Mock out the meta-data calls to the agent after checkout
	agent := tester.MockAgent(t)
	agent.Expect("meta-data", "exists", job.CommitMetadataKey).AndExitWith(1)
	agent.Expect("meta-data", "set", job.CommitMetadataKey).WithStdin(commitPattern)

	tester.RunAndCheck(t, env...)
}

func TestWithResolvingCommitExperiment_WithGitMirrors(t *testing.T) {
	t.Parallel()

	ctx, _ := experiments.Enable(mainCtx, experiments.ResolveCommitAfterCheckout)
	tester, err := NewExecutorTester(ctx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close()

	if err := tester.EnableGitMirrors(); err != nil {
		t.Fatalf("EnableGitMirrors() error = %v", err)
	}

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
	git.ExpectAll([][]any{
		{"clone", "--mirror", "--bare", "--", tester.Repo.Path, matchSubDir(tester.GitMirrorsDir)},
		{"clone", "-v", "--reference", matchSubDir(tester.GitMirrorsDir), "--", tester.Repo.Path, "."},
		{"clean", "-fdq"},
		{"fetch", "-v", "--", "origin", "main"},
		{"checkout", "-f", "FETCH_HEAD"},
		{"clean", "-fdq"},
		{"rev-parse", "HEAD"},
	})

	tester.RunAndCheck(t, env...)
}

func TestCheckingOutLocalGitProject_WithGitMirrors(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close()

	if err := tester.EnableGitMirrors(); err != nil {
		t.Fatalf("EnableGitMirrors() error = %v", err)
	}

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
	git.ExpectAll([][]any{
		{"clone", "--mirror", "--config", "pack.threads=35", "--", tester.Repo.Path, matchSubDir(tester.GitMirrorsDir)},
		{"clone", "-v", "--reference", matchSubDir(tester.GitMirrorsDir), "--", tester.Repo.Path, "."},
		{"clean", "-fdq"},
		{"fetch", "-v", "--", "origin", "main"},
		{"checkout", "-f", "FETCH_HEAD"},
		{"clean", "-fdq"},
		{"--no-pager", "log", "-1", "HEAD", "-s", "--no-color", gitShowFormatArg},
	})

	// Mock out the meta-data calls to the agent after checkout
	agent := tester.MockAgent(t)
	agent.Expect("meta-data", "exists", job.CommitMetadataKey).AndExitWith(1)
	agent.Expect("meta-data", "set", job.CommitMetadataKey).WithStdin(commitPattern)

	tester.RunAndCheck(t, env...)
}

func TestCheckingOutLocalGitProjectWithSubmodules_WithGitMirrors(t *testing.T) {
	t.Parallel()

	// Git for windows seems to struggle with local submodules in the temp dir
	if runtime.GOOS == "windows" {
		t.Skip()
	}

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close()

	if err := tester.EnableGitMirrors(); err != nil {
		t.Fatalf("EnableGitMirrors() error = %v", err)
	}

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
		{"--no-pager", "log", "-1", "HEAD", "-s", "--no-color", gitShowFormatArg},
	})

	// Mock out the meta-data calls to the agent after checkout
	agent := tester.MockAgent(t)
	agent.Expect("meta-data", "exists", job.CommitMetadataKey).AndExitWith(1)
	agent.Expect("meta-data", "set", job.CommitMetadataKey).WithStdin(commitPattern)

	tester.RunAndCheck(t, env...)
}

func TestCheckingOutLocalGitProjectWithSubmodulesDisabled_WithGitMirrors(t *testing.T) {
	t.Parallel()

	// Git for windows seems to struggle with local submodules in the temp dir
	if runtime.GOOS == "windows" {
		t.Skip()
	}

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close()

	if err := tester.EnableGitMirrors(); err != nil {
		t.Fatalf("EnableGitMirrors() error = %v", err)
	}

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
	git.ExpectAll([][]any{
		{"clone", "--mirror", "-v", "--", tester.Repo.Path, matchSubDir(tester.GitMirrorsDir)},
		{"clone", "-v", "--reference", matchSubDir(tester.GitMirrorsDir), "--", tester.Repo.Path, "."},
		{"clean", "-fdq"},
		{"submodule", "foreach", "--recursive", "git clean -fdq"},
		{"fetch", "-v", "--", "origin", "main"},
		{"checkout", "-f", "FETCH_HEAD"},
		{"clean", "-fdq"},
		{"--no-pager", "log", "-1", "HEAD", "-s", "--no-color", gitShowFormatArg},
	})

	// Mock out the meta-data calls to the agent after checkout
	agent := tester.MockAgent(t)
	agent.Expect("meta-data", "exists", job.CommitMetadataKey).AndExitWith(1)
	agent.Expect("meta-data", "set", job.CommitMetadataKey).WithStdin(commitPattern)

	tester.RunAndCheck(t, env...)
}

func TestCheckingOutShallowCloneOfLocalGitProject_WithGitMirrors(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close()

	if err := tester.EnableGitMirrors(); err != nil {
		t.Fatalf("EnableGitMirrors() error = %v", err)
	}

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
	git.ExpectAll([][]any{
		{"clone", "--mirror", "--bare", "--", tester.Repo.Path, matchSubDir(tester.GitMirrorsDir)},
		{"clone", "--depth=1", "--reference", matchSubDir(tester.GitMirrorsDir), "--", tester.Repo.Path, "."},
		{"clean", "-fdq"},
		{"fetch", "--depth=1", "--", "origin", "main"},
		{"checkout", "-f", "FETCH_HEAD"},
		{"clean", "-fdq"},
		{"--no-pager", "log", "-1", "HEAD", "-s", "--no-color", gitShowFormatArg},
	})

	// Mock out the meta-data calls to the agent after checkout
	agent := tester.MockAgent(t)
	agent.Expect("meta-data", "exists", job.CommitMetadataKey).AndExitWith(1)
	agent.Expect("meta-data", "set", job.CommitMetadataKey).WithStdin(commitPattern)

	tester.RunAndCheck(t, env...)
}

func TestCheckingOutSetsCorrectGitMetadataAndSendsItToBuildkite_WithGitMirrors(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close()

	if err := tester.EnableGitMirrors(); err != nil {
		t.Fatalf("EnableGitMirrors() error = %v", err)
	}

	agent := tester.MockAgent(t)
	agent.Expect("meta-data", "exists", job.CommitMetadataKey).AndExitWith(1)
	agent.Expect("meta-data", "set", job.CommitMetadataKey).WithStdin(commitPattern)

	tester.RunAndCheck(t)
}

func TestCheckingOutWithSSHKeyscan_WithGitMirrors(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close()

	if err := tester.EnableGitMirrors(); err != nil {
		t.Fatalf("EnableGitMirrors() error = %v", err)
	}

	var gitSSHCommand string
	git := tester.MustMock(t, "git")
	git.IgnoreUnexpectedInvocations()

	git.Expect("clone", "--mirror", "-v", "--", "git@github.com:buildkite/agent.git", bintest.MatchAny()).
		AndCallFunc(func(c *bintest.Call) {
			// Capture GIT_SSH_COMMAND for verification
			for _, env := range c.Env {
				if strings.HasPrefix(env, "GIT_SSH_COMMAND=") {
					gitSSHCommand = env
					break
				}
			}
			c.Exit(0)
		})

	env := []string{
		"BUILDKITE_REPO=git@github.com:buildkite/agent.git",
		"BUILDKITE_SSH_KEYSCAN=true",
	}

	tester.RunAndCheck(t, env...)

	// Verify GIT_SSH_COMMAND was set with accept-new
	if !strings.Contains(gitSSHCommand, "StrictHostKeyChecking=accept-new") {
		t.Errorf("Expected GIT_SSH_COMMAND to contain 'StrictHostKeyChecking=accept-new', got: %q", gitSSHCommand)
	}
}

func TestCheckingOutWithoutSSHKeyscan_WithGitMirrors(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close()

	if err := tester.EnableGitMirrors(); err != nil {
		t.Fatalf("EnableGitMirrors() error = %v", err)
	}

	var gitSSHCommand string
	git := tester.MustMock(t, "git")
	git.IgnoreUnexpectedInvocations()

	git.Expect("clone", "--mirror", "-v", "--", "https://github.com/buildkite/bash-example.git", bintest.MatchAny()).
		AndCallFunc(func(c *bintest.Call) {
			// Capture GIT_SSH_COMMAND for verification
			for _, env := range c.Env {
				if strings.HasPrefix(env, "GIT_SSH_COMMAND=") {
					gitSSHCommand = env
					break
				}
			}
			c.Exit(0)
		})

	env := []string{
		"BUILDKITE_REPO=https://github.com/buildkite/bash-example.git",
		"BUILDKITE_SSH_KEYSCAN=false",
	}

	tester.RunAndCheck(t, env...)

	// Verify GIT_SSH_COMMAND does NOT contain accept-new when SSHKeyscan is disabled
	if strings.Contains(gitSSHCommand, "StrictHostKeyChecking=accept-new") {
		t.Errorf("Expected GIT_SSH_COMMAND to NOT contain 'StrictHostKeyChecking=accept-new', got: %q", gitSSHCommand)
	}
}

func TestCheckingOutWithSSHKeyscanAndHTTPSRepo_WithGitMirrors(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close()

	if err := tester.EnableGitMirrors(); err != nil {
		t.Fatalf("EnableGitMirrors() error = %v", err)
	}

	var gitSSHCommand string
	git := tester.MustMock(t, "git")
	git.IgnoreUnexpectedInvocations()

	// Even with HTTPS repos, GIT_SSH_COMMAND is set (it just won't be used)
	git.Expect("clone", "--mirror", "-v", "--", "https://github.com/buildkite/bash-example.git", bintest.MatchAny()).
		AndCallFunc(func(c *bintest.Call) {
			// Capture GIT_SSH_COMMAND for verification
			for _, env := range c.Env {
				if strings.HasPrefix(env, "GIT_SSH_COMMAND=") {
					gitSSHCommand = env
					break
				}
			}
			c.Exit(0)
		})

	env := []string{
		"BUILDKITE_REPO=https://github.com/buildkite/bash-example.git",
		"BUILDKITE_SSH_KEYSCAN=true",
	}

	tester.RunAndCheck(t, env...)

	// Verify GIT_SSH_COMMAND was set with accept-new even for HTTPS repos
	if !strings.Contains(gitSSHCommand, "StrictHostKeyChecking=accept-new") {
		t.Errorf("Expected GIT_SSH_COMMAND to contain 'StrictHostKeyChecking=accept-new', got: %q", gitSSHCommand)
	}
}

func TestCleaningAnExistingCheckout_WithGitMirrors(t *testing.T) {
	t.Skip("TODO: Figure out what this test is actually supposed to be testing")

	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close()

	if err := tester.EnableGitMirrors(); err != nil {
		t.Fatalf("EnableGitMirrors() error = %v", err)
	}

	// Create an existing checkout
	out, err := tester.Repo.Execute("clone", "-v", "--", tester.Repo.Path, tester.CheckoutDir())
	if err != nil {
		t.Fatalf(`tester.Repo.Execute(clone, -v, --, %q, %q) error = %v\nout = %s`, tester.Repo.Path, tester.CheckoutDir(), err, out)
	}
	testpath := filepath.Join(tester.CheckoutDir(), "test.txt")
	if err := os.WriteFile(testpath, []byte("llamas"), 0o700); err != nil {
		t.Fatalf("os.WriteFile(test.txt, llamas, 0o700) = %v", err)
	}

	// Mock out the meta-data calls to the agent after checkout
	agent := tester.MockAgent(t)
	agent.
		Expect("meta-data", "exists", job.CommitMetadataKey).
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

func TestForcingACleanCheckout_WithGitMirrors(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close()

	if err := tester.EnableGitMirrors(); err != nil {
		t.Fatalf("EnableGitMirrors() error = %v", err)
	}

	// Mock out the meta-data calls to the agent after checkout
	agent := tester.MockAgent(t)
	agent.
		Expect("meta-data", "exists", job.CommitMetadataKey).
		AndExitWith(0)

	tester.RunAndCheck(t, "BUILDKITE_CLEAN_CHECKOUT=true")

	if !strings.Contains(tester.Output, "Cleaning pipeline checkout") {
		t.Fatal(`tester.Output does not contain "Cleaning pipeline checkout"`)
	}
}

func TestCheckoutOnAnExistingRepositoryWithoutAGitFolder_WithGitMirrors(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close()

	if err := tester.EnableGitMirrors(); err != nil {
		t.Fatalf("EnableGitMirrors() error = %v", err)
	}

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
		Expect("meta-data", "exists", job.CommitMetadataKey).
		AndExitWith(0)

	tester.RunAndCheck(t)
}

func TestCheckoutRetriesOnCleanFailure_WithGitMirrors(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close()

	if err := tester.EnableGitMirrors(); err != nil {
		t.Fatalf("EnableGitMirrors() error = %v", err)
	}

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

func TestCheckoutRetriesOnCloneFailure_WithGitMirrors(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close()

	if err := tester.EnableGitMirrors(); err != nil {
		t.Fatalf("EnableGitMirrors() error = %v", err)
	}

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

func TestCheckoutDoesNotRetryOnHookFailure_WithGitMirrors(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close()

	if err := tester.EnableGitMirrors(); err != nil {
		t.Fatalf("EnableGitMirrors() error = %v", err)
	}

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

func TestRepositorylessCheckout_WithGitMirrors(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Not supported on windows")
	}

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close()

	if err := tester.EnableGitMirrors(); err != nil {
		t.Fatalf("EnableGitMirrors() error = %v", err)
	}

	script := []string{
		"#!/usr/bin/env bash",
		"export BUILDKITE_REPO=",
	}

	if err := os.WriteFile(filepath.Join(tester.HooksDir, "environment"), []byte(strings.Join(script, "\n")), 0o700); err != nil {
		t.Fatalf("os.WriteFile(environment, script, 0o700) = %v", err)
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
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close()

	if err := tester.EnableGitMirrors(); err != nil {
		t.Fatalf("EnableGitMirrors() error = %v", err)
	}

	// assert the correct BUILDKITE_REPO_MIRROR _after_ the bootstrap has run
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
	git.ExpectAll([][]any{
		{"clone", "--mirror", "--bare", "--", tester.Repo.Path, matchSubDir(tester.GitMirrorsDir)},
		{"clone", "-v", "--reference", matchSubDir(tester.GitMirrorsDir), "--", tester.Repo.Path, "."},
		{"clean", "-fdq"},
		{"fetch", "-v", "--", "origin", "main"},
		{"checkout", "-f", "FETCH_HEAD"},
		{"clean", "-fdq"},
		{"--no-pager", "log", "-1", "HEAD", "-s", "--no-color", gitShowFormatArg},
	})

	// Mock out the meta-data calls to the agent after checkout
	agent := tester.MockAgent(t)
	agent.Expect("meta-data", "exists", job.CommitMetadataKey).AndExitWith(1)
	agent.Expect("meta-data", "set", job.CommitMetadataKey).WithStdin(commitPattern)

	tester.RunAndCheck(t, env...)

	if !strings.HasPrefix(gitMirrorPath, tester.GitMirrorsDir) {
		t.Errorf("gitMirrorPath = %q, want prefix %q", gitMirrorPath, tester.GitMirrorsDir)
	}
}

func TestCheckingOutWithCustomRefspec_WithGitMirrors(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close()

	if err := tester.EnableGitMirrors(); err != nil {
		t.Fatalf("EnableGitMirrors() error = %v", err)
	}

	// Create a custom ref in the repository that's different from main
	customRef := "refs/custom/test-ref"
	out, err := tester.Repo.Execute("update-ref", customRef, "HEAD")
	if err != nil {
		t.Fatalf("tester.Repo.Execute(update-ref, %q, HEAD) error = %v\nout = %s", customRef, err, out)
	}

	// Create a new commit on a branch that's not in the custom ref
	out, err = tester.Repo.Execute("checkout", "-b", "different-branch")
	if err != nil {
		t.Fatalf("tester.Repo.Execute(checkout, -b, different-branch) error = %v\nout = %s", err, out)
	}

	differentFile := "different.txt"
	if err := os.WriteFile(filepath.Join(tester.Repo.Path, differentFile), []byte("different content"), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%s, different content, 0o600) = %v", differentFile, err)
	}

	out, err = tester.Repo.Execute("add", differentFile)
	if err != nil {
		t.Fatalf("tester.Repo.Execute(add, %s) error = %v\nout = %s", differentFile, err, out)
	}

	out, err = tester.Repo.Execute("commit", "-m", "Different commit")
	if err != nil {
		t.Fatalf("tester.Repo.Execute(commit, -m, Different commit) error = %v\nout = %s", err, out)
	}

	// Go back to main
	out, err = tester.Repo.Execute("checkout", "main")
	if err != nil {
		t.Fatalf("tester.Repo.Execute(checkout, main) error = %v\nout = %s", err, out)
	}

	env := []string{
		"BUILDKITE_REFSPEC=" + customRef,
		"BUILDKITE_GIT_CLONE_MIRROR_FLAGS=--bare",
	}

	// Actually execute git commands, but with expectations
	git := tester.
		MustMock(t, "git").
		PassthroughToLocalCommand()

	// With the fix: the mirror should fetch the custom refspec instead of the branch
	git.ExpectAll([][]any{
		{"clone", "--mirror", "--bare", "--", tester.Repo.Path, matchSubDir(tester.GitMirrorsDir)},
		{"clone", "-v", "--reference", matchSubDir(tester.GitMirrorsDir), "--", tester.Repo.Path, "."},
		{"clean", "-ffxdq"},
		{"fetch", "--", "origin", customRef}, // Mirror fetches custom refspec (correct!)
		{"checkout", "-f", "FETCH_HEAD"},
		{"clean", "-ffxdq"},
		{"--no-pager", "log", "-1", "HEAD", "-s", "--no-color", gitShowFormatArg},
	})

	// Mock out the meta-data calls to the agent after checkout
	agent := tester.MockAgent(t)
	agent.Expect("meta-data", "exists", job.CommitMetadataKey).AndExitWith(1)
	agent.Expect("meta-data", "set", job.CommitMetadataKey).WithStdin(commitPattern)

	tester.RunAndCheck(t, env...)
}
