package integration

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/buildkite/agent/v3/internal/experiments"
	"github.com/buildkite/agent/v3/internal/job"
	"github.com/buildkite/bintest/v3"
	"gotest.tools/v3/assert"
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
const gitShowFormatArg = "--format=commit %H%nabbrev-commit %h%nAuthor: %an <%ae>%n%n%w(0,4,4)%B"

func TestWithResolvingCommitExperiment(t *testing.T) {
	t.Parallel()

	ctx, _ := experiments.Enable(mainCtx, experiments.ResolveCommitAfterCheckout)
	tester, err := NewExecutorTester(ctx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

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
		{"clone", "-v", "--", tester.Repo.Path, "."},
		{"clean", "-fdq"},
		{"fetch", "-v", "--", "origin", "main"},
		{"checkout", "-f", "FETCH_HEAD"},
		{"clean", "-fdq"},
		{"rev-parse", "HEAD"},
	})

	tester.RunAndCheck(t, env...)
}

func TestCheckingOutLocalGitProject(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

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
		{"clone", "-v", "--", tester.Repo.Path, "."},
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

func TestCheckingOutLocalGitProjectWithSubmodules(t *testing.T) {
	t.Parallel()

	// Git for windows seems to struggle with local submodules in the temp dir
	if runtime.GOOS == "windows" {
		t.Skip()
	}

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

	submoduleRepo, err := createTestGitRespository()
	if err != nil {
		t.Fatalf("createTestGitRepository() error = %v", err)
	}
	defer submoduleRepo.Close() //nolint:errcheck // best-effort cleanup in test

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
		{"--no-pager", "log", "-1", "HEAD", "-s", "--no-color", gitShowFormatArg},
	})

	// Mock out the meta-data calls to the agent after checkout
	agent := tester.MockAgent(t)
	agent.Expect("meta-data", "exists", job.CommitMetadataKey).AndExitWith(1)
	agent.Expect("meta-data", "set", job.CommitMetadataKey).WithStdin(commitPattern)

	tester.RunAndCheck(t, env...)
}

func TestCheckingOutLocalGitProjectWithSubmodulesDisabled(t *testing.T) {
	t.Parallel()

	// Git for windows seems to struggle with local submodules in the temp dir
	if runtime.GOOS == "windows" {
		t.Skip()
	}

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

	submoduleRepo, err := createTestGitRespository()
	if err != nil {
		t.Fatalf("createTestGitRespository() error = %v", err)
	}
	defer submoduleRepo.Close() //nolint:errcheck // best-effort cleanup in test

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
		{"clone", "-v", "--", tester.Repo.Path, "."},
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

func TestCheckingOutShallowCloneOfLocalGitProject(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

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
		{"clone", "--depth=1", "--", tester.Repo.Path, "."},
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

func TestCheckingOutLocalGitProjectWithShortCommitHash(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	assert.NilError(t, err)
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

	// Do one checkout
	tester.RunAndCheck(t)

	// Create another commit on the same branch in the remote repo
	err = tester.Repo.ExecuteAll([][]string{
		{"commit", "--allow-empty", "-m", "Another commit"},
	})
	assert.NilError(t, err)

	commitHash, err := tester.Repo.RevParse("HEAD")
	assert.NilError(t, err)
	shortCommitHash := commitHash[:7]

	git := tester.
		MustMock(t, "git").
		PassthroughToLocalCommand()

	// Git should attempt to fetch the shortHash, but fail. Then fallback to fetching
	// all the heads and tags and checking out the short commit hash.
	git.ExpectAll([][]any{
		{"remote", "get-url", "origin"},
		{"clean", "-ffxdq"},
		{"fetch", "--", "origin", shortCommitHash},
		{"config", "remote.origin.fetch"},
		{"fetch", "--", "origin", "+refs/heads/*:refs/remotes/origin/*", "+refs/tags/*:refs/tags/*"},
		{"checkout", "-f", shortCommitHash},
		{"clean", "-ffxdq"},
		{"--no-pager", "log", "-1", shortCommitHash, "-s", "--no-color", gitShowFormatArg},
	})

	// Mock out the meta-data calls to the agent after checkout
	agent := tester.MockAgent(t)
	agent.Expect("meta-data", "exists", job.CommitMetadataKey).AndExitWith(1)
	agent.Expect("meta-data", "set", job.CommitMetadataKey).WithStdin(commitPattern)

	env := []string{
		fmt.Sprintf("BUILDKITE_COMMIT=%s", shortCommitHash),
	}
	tester.RunAndCheck(t, env...)

	// Check state of the checkout directory
	checkoutRepo := &gitRepository{Path: tester.CheckoutDir()}
	checkoutRepoCommit, err := checkoutRepo.RevParse("HEAD")
	assert.NilError(t, err)

	assert.Equal(t, checkoutRepoCommit, commitHash)
}

func TestCheckingOutGitHubPullRequestWithCommitHash(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

	commitHash, err := tester.Repo.RevParse("refs/pull/123/head")
	assert.NilError(t, err)

	env := []string{
		"BUILDKITE_GIT_CLONE_FLAGS=--no-local", // Disable the fast local clone method, which automatically copies all refs
		"BUILDKITE_BRANCH=update-test-txt",
		"BUILDKITE_PULL_REQUEST=123",
		"BUILDKITE_PIPELINE_PROVIDER=github",
		fmt.Sprintf("BUILDKITE_COMMIT=%s", strings.TrimSpace(commitHash)),
	}

	tester.RunAndCheck(t, env...)

	// Check state of the checkout directory
	checkoutRepo := &gitRepository{Path: tester.CheckoutDir()}
	checkoutRepoCommit, err := checkoutRepo.RevParse("HEAD")
	assert.NilError(t, err)
	assert.Equal(t, checkoutRepoCommit, commitHash)
}

func TestCheckingOutGitHubPullRequestAndCustomRefmap(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

	commitHash, err := tester.Repo.RevParse("refs/pull/123/head")
	assert.NilError(t, err)

	env := []string{
		"BUILDKITE_GIT_CLONE_FLAGS=--no-local",                               // Disable the fast local clone method, which automatically copies all refs
		"BUILDKITE_GIT_FETCH_FLAGS=--refmap=+refs/pull/*:refs/pull/origin/*", // Track remote pull request refs locally
		"BUILDKITE_BRANCH=update-test-txt",
		"BUILDKITE_PULL_REQUEST=123",
		"BUILDKITE_PIPELINE_PROVIDER=github",
		fmt.Sprintf("BUILDKITE_COMMIT=%s", strings.TrimSpace(commitHash)),
	}

	tester.RunAndCheck(t, env...)

	// Check state of the checkout directory
	checkoutRepo := &gitRepository{Path: tester.CheckoutDir()}
	checkoutRepoCommit, err := checkoutRepo.RevParse("HEAD")
	assert.NilError(t, err)
	assert.Equal(t, checkoutRepoCommit, commitHash)

	// This local ref should match remote refs/pull/123/head
	localPullRefCommit, err := checkoutRepo.RevParse("refs/pull/origin/123/head")
	assert.NilError(t, err)
	assert.Equal(t, localPullRefCommit, commitHash)
}

func TestCheckingOutGitHubPullRequestWithCommitHashAfterForcePush(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

	commitHash, err := tester.Repo.RevParse("refs/pull/123/head")
	assert.NilError(t, err)

	env := []string{
		"BUILDKITE_GIT_CLONE_FLAGS=--no-local", // Disable the fast local clone method, which automatically copies all refs
		"BUILDKITE_BRANCH=update-test-txt",
		"BUILDKITE_PULL_REQUEST=123",
		"BUILDKITE_PIPELINE_PROVIDER=github",
		fmt.Sprintf("BUILDKITE_COMMIT=%s", strings.TrimSpace(commitHash)),
	}

	// Amend the pull request, so commitHash is no longer reachable from refs/pull/123/head
	err = tester.Repo.CheckoutBranch("update-test-txt")
	assert.NilError(t, err)

	err = os.WriteFile(
		filepath.Join(tester.Repo.Path, "test.txt"),
		[]byte("This is an amended test pull request"),
		0o600,
	)
	assert.NilError(t, err)

	err = tester.Repo.Add("test.txt")
	assert.NilError(t, err)

	_, err = tester.Repo.Execute("commit", "--amend", "-m", "Amended PR Commit")
	assert.NilError(t, err)

	_, err = tester.Repo.Execute("update-ref", "refs/pull/123/head", "HEAD")
	assert.NilError(t, err)

	tester.RunAndCheck(t, env...)

	// Check state of the checkout directory
	checkoutRepo := &gitRepository{Path: tester.CheckoutDir()}
	checkoutRepoCommit, err := checkoutRepo.RevParse("HEAD")
	assert.NilError(t, err)
	assert.Equal(t, checkoutRepoCommit, commitHash)
}

func TestCheckingOutGitHubPullRequestWithShortCommitHash(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

	commitHash, err := tester.Repo.RevParse("refs/pull/123/head")
	assert.NilError(t, err)
	shortCommitHash := commitHash[:7]

	env := []string{
		"BUILDKITE_GIT_CLONE_FLAGS=--no-local", // Disable the fast local clone method, which automatically copies all refs
		"BUILDKITE_BRANCH=update-test-txt",
		"BUILDKITE_PULL_REQUEST=123",
		"BUILDKITE_PIPELINE_PROVIDER=github",
		fmt.Sprintf("BUILDKITE_COMMIT=%s", shortCommitHash),
	}

	tester.RunAndCheck(t, env...)

	// Check state of the checkout directory
	checkoutRepo := &gitRepository{Path: tester.CheckoutDir()}
	checkoutRepoCommit, err := checkoutRepo.RevParse("HEAD")
	assert.NilError(t, err)
	assert.Equal(t, checkoutRepoCommit, commitHash)
}

func TestCheckingOutGitHubPullRequestAtHead(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

	commitHash, err := tester.Repo.RevParse("refs/pull/123/head")
	assert.NilError(t, err)

	env := []string{
		"BUILDKITE_GIT_CLONE_FLAGS=--no-local", // Disable the fast local clone method, which automatically copies all refs
		"BUILDKITE_BRANCH=update-test-txt",
		"BUILDKITE_PULL_REQUEST=123",
		"BUILDKITE_PIPELINE_PROVIDER=github",
		"BUILDKITE_COMMIT=HEAD",
	}

	tester.RunAndCheck(t, env...)

	// Check state of the checkout directory
	checkoutRepo := &gitRepository{Path: tester.CheckoutDir()}
	checkoutRepoCommit, err := checkoutRepo.RevParse("HEAD")
	assert.NilError(t, err)
	assert.Equal(t, checkoutRepoCommit, commitHash)
}

func TestCheckingOutGitHubPullRequestMergeRefspec(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

	commitHash, err := tester.Repo.RevParse("refs/pull/123/merge")
	assert.NilError(t, err)

	env := []string{
		"BUILDKITE_GIT_CLONE_FLAGS=--no-local", // Disable the fast local clone method, which automatically copies all refs
		"BUILDKITE_BRANCH=update-test-txt",
		"BUILDKITE_PULL_REQUEST=123",
		"BUILDKITE_PIPELINE_PROVIDER=github",
		"BUILDKITE_PULL_REQUEST_USING_MERGE_REFSPEC=true",
	}

	git := tester.
		MustMock(t, "git").
		PassthroughToLocalCommand()

	git.ExpectAll([][]any{
		{"clone", "--no-local", "--", tester.Repo.Path, "."},
		{"clean", "-ffxdq"},
		{"fetch", "--", "origin", "refs/pull/123/merge"},
		{"checkout", "-f", "FETCH_HEAD"},
		{"clean", "-ffxdq"},
		{"rev-parse", "FETCH_HEAD"},
	})

	tester.RunAndCheck(t, env...)

	// Check state of the checkout directory
	checkoutRepo := &gitRepository{Path: tester.CheckoutDir()}
	checkoutRepoCommit, err := checkoutRepo.RevParse("HEAD")
	assert.NilError(t, err)
	assert.Equal(t, checkoutRepoCommit, commitHash)
}

func TestCheckingOutGitHubPullRequestAtHeadFromFork(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

	commitHash, err := tester.Repo.RevParse("refs/pull/123/head")
	assert.NilError(t, err)

	// Remove the branch ref to simulate this being a PR from a fork
	_, err = tester.Repo.Execute("branch", "-D", "update-test-txt")
	assert.NilError(t, err)

	env := []string{
		"BUILDKITE_GIT_CLONE_FLAGS=--no-local", // Disable the fast local clone method, which automatically copies all refs
		"BUILDKITE_BRANCH=forker:update-test-txt",
		"BUILDKITE_PULL_REQUEST=123",
		"BUILDKITE_PIPELINE_PROVIDER=github",
		"BUILDKITE_COMMIT=HEAD",
	}

	tester.RunAndCheck(t, env...)

	// Check state of the checkout directory
	checkoutRepo := &gitRepository{Path: tester.CheckoutDir()}
	checkoutRepoCommit, err := checkoutRepo.RevParse("HEAD")
	assert.NilError(t, err)
	assert.Equal(t, checkoutRepoCommit, commitHash)
}

func TestCheckoutErrorIsRetried(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

	env := []string{
		"BUILDKITE_GIT_CLONE_FLAGS=-v",
		"BUILDKITE_GIT_CLEAN_FLAGS=-fdq",
		"BUILDKITE_GIT_FETCH_FLAGS=-v",
	}

	// Simulate state from a previous checkout
	if err := os.MkdirAll(tester.CheckoutDir(), 0o755); err != nil {
		t.Fatalf("error creating dir to clone from: %s", err)
	}
	cmd := exec.Command("git", "clone", "-v", "--", tester.Repo.Path, ".")
	cmd.Dir = tester.CheckoutDir()
	if _, err = cmd.Output(); err != nil {
		t.Fatalf("error cloning test repo: %s", err)
	}

	// Make the git dir dirty to simulate a SIGKILLed git process
	gitDir := path.Join(tester.CheckoutDir(), ".git")
	lockFilePath := path.Join(gitDir, "index.lock")
	f, err := os.Create(lockFilePath)
	if err != nil {
		t.Fatalf("error creating lock file: %s", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("error closing lock file: %s", err)
	}

	// Actually execute git commands, but with expectations
	git := tester.
		MustMock(t, "git").
		PassthroughToLocalCommand()

	// But assert which ones are called
	git.ExpectAll([][]any{
		{"remote", "get-url", "origin"},
		{"clean", "-fdq"},
		{"fetch", "-v", "--", "origin", "main"},
		{"checkout", "-f", "FETCH_HEAD"},
		{"clone", "-v", "--", tester.Repo.Path, "."},
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

func TestFetchErrorIsRetried(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

	env := []string{
		"BUILDKITE_GIT_CLONE_FLAGS=-v --depth=1",
		"BUILDKITE_GIT_CLEAN_FLAGS=-ffxdq",
		"BUILDKITE_GIT_FETCH_FLAGS=-v --prune --depth=1",
	}

	// Simulate state from a previous checkout
	if err := os.MkdirAll(tester.CheckoutDir(), 0o755); err != nil {
		t.Fatalf("error creating dir to clone from: %s", err)
	}
	cmd := exec.Command("git", "clone", "-v", "--", tester.Repo.Path, ".")
	cmd.Dir = tester.CheckoutDir()
	if _, err = cmd.Output(); err != nil {
		t.Fatalf("error cloning test repo: %s", err)
	}

	// Make the git dir dirty to simulate a SIGKILLed git process
	gitDir := path.Join(tester.CheckoutDir(), ".git")
	lockFilePath := path.Join(gitDir, "shallow.lock")
	f, err := os.Create(lockFilePath)
	if err != nil {
		t.Fatalf("error creating lock file: %s", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("error closing lock file: %s", err)
	}

	// Actually execute git commands, but with expectations
	git := tester.
		MustMock(t, "git").
		PassthroughToLocalCommand()

	// But assert which ones are called
	git.ExpectAll([][]any{
		{"remote", "get-url", "origin"},
		{"clean", "-ffxdq"},
		{"fetch", "-v", "--prune", "--depth=1", "--", "origin", "main"},
		{"clone", "-v", "--depth=1", "--", tester.Repo.Path, "."},
		{"clean", "-ffxdq"},
		{"fetch", "-v", "--prune", "--depth=1", "--", "origin", "main"},
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

func TestCheckingOutSetsCorrectGitMetadataAndSendsItToBuildkite(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

	agent := tester.MockAgent(t)
	agent.Expect("meta-data", "exists", job.CommitMetadataKey).AndExitWith(1)
	agent.Expect("meta-data", "set", job.CommitMetadataKey).WithStdin(commitPattern)

	tester.RunAndCheck(t)
}

func TestCheckingOutWithSSHKeyscan(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

	tester.MustMock(t, "ssh-keyscan").
		Expect("github.com").
		AndWriteToStdout("github.com ssh-rsa xxx=").
		AndExitWith(0)

	git := tester.MustMock(t, "git")
	git.IgnoreUnexpectedInvocations()

	git.Expect("clone", "-v", "--", "git@github.com:buildkite/agent.git", ".").
		AndExitWith(0)

	env := []string{
		"BUILDKITE_REPO=git@github.com:buildkite/agent.git",
		"BUILDKITE_SSH_KEYSCAN=true",
	}

	tester.RunAndCheck(t, env...)
}

func TestCheckingOutWithoutSSHKeyscan(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

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

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

	tester.MustMock(t, "ssh-keyscan").
		Expect("github.com").
		NotCalled()

	git := tester.MustMock(t, "git")
	git.IgnoreUnexpectedInvocations()

	git.Expect("clone", "-v", "--", "https://github.com/buildkite/bash-example.git", ".").
		AndExitWith(0)

	env := []string{
		"BUILDKITE_REPO=https://github.com/buildkite/bash-example.git",
		"BUILDKITE_SSH_KEYSCAN=true",
	}

	tester.RunAndCheck(t, env...)
}

func TestCleaningAnExistingCheckout(t *testing.T) {
	t.Skip("TODO: Figure out what this test is actually supposed to be testing")

	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

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

func TestForcingACleanCheckout(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

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

func TestSkippingCheckout(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

	tester.RunAndCheck(t, "BUILDKITE_SKIP_CHECKOUT=true")

	if !strings.Contains(tester.Output, "Skipping checkout") {
		t.Fatal(`tester.Output does not contain "Skipping checkout"`)
	}

	// Verify no git commands were run (no clone, fetch, checkout)
	if strings.Contains(tester.Output, "git clone") {
		t.Fatal(`tester.Output should not contain "git clone" when checkout is skipped`)
	}
}

func TestCheckoutOnAnExistingRepositoryWithoutAGitFolder(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

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

func TestCheckoutRetriesOnCleanFailure(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

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
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

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
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

	var checkoutCounter int32

	tester.ExpectGlobalHook("checkout").Once().AndCallFunc(func(c *bintest.Call) {
		counter := atomic.AddInt32(&checkoutCounter, 1)
		fmt.Fprintf(c.Stdout, "Checkout invocation %d\n", counter) //nolint:errcheck // test helper; write error is non-actionable
		if counter == 1 {
			fmt.Fprintf(c.Stdout, "Sunspots have caused checkout to fail\n") //nolint:errcheck // test helper; write error is non-actionable
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

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

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

func TestGitCheckoutWithCommitResolved(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

	env := []string{"BUILDKITE_COMMIT_RESOLVED=true"}

	git := tester.MustMock(t, "git").PassthroughToLocalCommand()

	git.ExpectAll([][]any{
		{"clone", "-v", "--", tester.Repo.Path, "."},
		{"clean", "-ffxdq"},
		{"fetch", "--", "origin", "main"},
		{"checkout", "-f", "FETCH_HEAD"},
		{"clean", "-ffxdq"},
	})

	agent := tester.MockAgent(t)
	agent.Expect("meta-data", "exists", job.CommitMetadataKey).Exactly(0)

	tester.RunAndCheck(t, env...)
}

func TestGitCheckoutWithoutCommitResolved(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

	env := []string{"BUILDKITE_COMMIT_RESOLVED=false"}

	git := tester.MustMock(t, "git").PassthroughToLocalCommand()

	git.ExpectAll([][]any{
		{"clone", "-v", "--", tester.Repo.Path, "."},
		{"clean", "-ffxdq"},
		{"fetch", "--", "origin", "main"},
		{"checkout", "-f", "FETCH_HEAD"},
		{"clean", "-ffxdq"},
	})

	agent := tester.MockAgent(t)
	agent.Expect("meta-data", "exists", job.CommitMetadataKey).AndExitWith(0).Exactly(1)

	tester.RunAndCheck(t, env...)
}

func TestGitCheckoutWithoutCommitResolvedAndNoMetaData(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

	env := []string{"BUILDKITE_COMMIT_RESOLVED=false"}

	git := tester.MustMock(t, "git").PassthroughToLocalCommand()

	git.ExpectAll([][]any{
		{"clone", "-v", "--", tester.Repo.Path, "."},
		{"clean", "-ffxdq"},
		{"fetch", "--", "origin", "main"},
		{"checkout", "-f", "FETCH_HEAD"},
		{"clean", "-ffxdq"},
		{"--no-pager", "log", "-1", "HEAD", "-s", "--no-color", gitShowFormatArg},
	})

	agent := tester.MockAgent(t)
	agent.Expect("meta-data", "exists", job.CommitMetadataKey).AndExitWith(1).Exactly(1)
	agent.Expect("meta-data", "set", job.CommitMetadataKey).WithStdin(commitPattern)

	tester.RunAndCheck(t, env...)
}

type subDirMatcher struct {
	dir string
}

func (mf subDirMatcher) Match(s string) (bool, string) {
	if filepath.Dir(filepath.Clean(s)) == mf.dir {
		return true, ""
	}
	return false, fmt.Sprintf("%q wasn't a sub directory of %q", s, mf.dir)
}

func (mf subDirMatcher) String() string {
	return fmt.Sprintf("subDirMatcher(%q)", mf.dir)
}

func matchSubDir(dir string) bintest.Matcher {
	return subDirMatcher{dir: filepath.Clean(dir)}
}
