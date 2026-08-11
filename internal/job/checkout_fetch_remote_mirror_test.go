package job

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/buildkite/agent/v3/internal/osutil"
	"github.com/buildkite/agent/v3/internal/process"
	"github.com/buildkite/agent/v3/internal/shell"
)

func TestFetchSourceExistingCheckoutRemoteMirrorHitSkipsCanonical(t *testing.T) {
	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	checkout := cloneExistingCheckoutForRemoteMirrorTest(t, canonical.RepoURL("canonical"))
	commit, _, err := canonical.PushBranch("canonical", "feature-branch")
	if err != nil {
		t.Fatal(err)
	}
	mirror := copyOnHostMirrorHTTPRepo(t, canonical.RepoURL("canonical"), "mirror")

	e, attempt := newExistingCheckoutRemoteMirrorExecutor(t, checkout, canonical.RepoURL("canonical"), mirror.RepoURL("mirror"), commit)
	canonical.Close()

	if err := e.fetchSource(t.Context(), false, &attempt); err != nil {
		t.Fatalf("fetchSource() error = %v", err)
	}
	if attempt.outcome != remoteMirrorOutcomeHit {
		t.Errorf("outcome = %q, want hit", attempt.outcome)
	}
	if !hasGitCommit(t.Context(), e.shell, ".git", commit) {
		t.Errorf("existing checkout does not contain mirror commit %s", commit)
	}
}

func TestFetchSourceExistingCheckoutPullRequestHeadHitSkipsCanonical(t *testing.T) {
	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	checkout := cloneExistingCheckoutForRemoteMirrorTest(t, canonical.RepoURL("canonical"))
	commit, _, err := canonical.PushBranch("canonical", "feature-branch")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := canonical.CreateRef("canonical", "refs/pull/123/head", commit); err != nil {
		t.Fatal(err)
	}
	// Fork shape: make the commit reachable only through refs/pull/*, so the
	// mirror probe cannot rely on an advertised branch tip.
	advancePullRequestHeadForMirrorTest(t, canonical.RepoURL("canonical"), commit)
	runGitForMirrorTest(t, checkout, "push", canonical.RepoURL("canonical"), "--delete", "feature-branch")
	mirror := copyOnHostMirrorHTTPRepo(t, canonical.RepoURL("canonical"), "mirror")

	e, attempt := newExistingCheckoutRemoteMirrorExecutor(t, checkout, canonical.RepoURL("canonical"), mirror.RepoURL("mirror"), commit)
	e.PullRequest = "123"
	e.PipelineProvider = "github"
	canonical.Close()

	if err := e.fetchSource(t.Context(), false, &attempt); err != nil {
		t.Fatalf("fetchSource() error = %v", err)
	}
	if attempt.outcome != remoteMirrorOutcomeHit {
		t.Errorf("outcome = %q, want hit", attempt.outcome)
	}
	if !hasGitCommit(t.Context(), e.shell, ".git", commit) {
		t.Errorf("existing checkout does not contain mirror commit %s", commit)
	}
	if got := gitConfigForRemoteMirrorTest(t, checkout, "remote.origin.promisor"); got != "" {
		t.Errorf("remote.origin.promisor = %q, want empty after mirror hit", got)
	}
}

func TestFetchSourceExistingCheckoutPullRequestHeadMissFallsBackToCanonical(t *testing.T) {
	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	checkout := cloneExistingCheckoutForRemoteMirrorTest(t, canonical.RepoURL("canonical"))
	// Copy the mirror before the pull request exists, so it lags canonical.
	mirror := copyOnHostMirrorHTTPRepo(t, canonical.RepoURL("canonical"), "mirror")
	commit, _, err := canonical.PushBranch("canonical", "feature-branch")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := canonical.CreateRef("canonical", "refs/pull/123/head", commit); err != nil {
		t.Fatal(err)
	}

	e, attempt := newExistingCheckoutRemoteMirrorExecutor(t, checkout, canonical.RepoURL("canonical"), mirror.RepoURL("mirror"), commit)
	e.PullRequest = "123"
	e.PipelineProvider = "github"

	if err := e.fetchSource(t.Context(), false, &attempt); err != nil {
		t.Fatalf("fetchSource() error = %v", err)
	}
	if attempt.outcome != remoteMirrorOutcomeMiss {
		t.Errorf("outcome = %q, want miss", attempt.outcome)
	}
	if !hasGitCommit(t.Context(), e.shell, ".git", commit) {
		t.Errorf("canonical pull request fallback did not fetch commit %s", commit)
	}
}

func TestFetchSourceExistingCheckoutRemoteMirrorMissLeavesStateUntouchedBeforeCanonicalFetch(t *testing.T) {
	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	checkout := cloneExistingCheckoutForRemoteMirrorTest(t, canonical.RepoURL("canonical"))
	mirror := copyOnHostMirrorHTTPRepo(t, canonical.RepoURL("canonical"), "mirror")
	commit, _, err := canonical.PushBranch("canonical", "feature-branch")
	if err != nil {
		t.Fatal(err)
	}

	configBefore := readRemoteMirrorTestFile(t, filepath.Join(checkout, ".git", "config"))
	refsBefore := gitOutputForRemoteCheckoutTest(t, checkout, "show-ref")
	worktreeBefore := gitOutputForRemoteCheckoutTest(t, checkout, "status", "--porcelain=v1", "--untracked-files=all")
	e, attempt := newExistingCheckoutRemoteMirrorExecutor(t, checkout, canonical.RepoURL("canonical"), mirror.RepoURL("mirror"), commit)

	if err := e.fetchSource(t.Context(), false, &attempt); err != nil {
		t.Fatalf("fetchSource() error = %v", err)
	}
	if attempt.outcome != remoteMirrorOutcomeMiss {
		t.Errorf("outcome = %q, want miss", attempt.outcome)
	}
	if got := readRemoteMirrorTestFile(t, filepath.Join(checkout, ".git", "config")); got != configBefore {
		t.Errorf(".git/config changed on mirror miss\nbefore:\n%s\nafter:\n%s", configBefore, got)
	}
	if got := gitOutputForRemoteCheckoutTest(t, checkout, "show-ref"); got != refsBefore {
		t.Errorf("refs changed on exact-SHA mirror miss\nbefore:\n%s\nafter:\n%s", refsBefore, got)
	}
	if got := gitOutputForRemoteCheckoutTest(t, checkout, "status", "--porcelain=v1", "--untracked-files=all"); got != worktreeBefore {
		t.Errorf("worktree changed on mirror miss\nbefore:\n%s\nafter:\n%s", worktreeBefore, got)
	}
	if !hasGitCommit(t.Context(), e.shell, ".git", commit) {
		t.Error("canonical fallback did not fetch requested commit")
	}
}

func TestFetchSourceExistingCheckoutFilteredLagMissRetargetsPromisor(t *testing.T) {
	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	if err := canonical.ConfigureRepository("canonical", "uploadpack.allowFilter", "true"); err != nil {
		t.Fatal(err)
	}
	if err := canonical.ConfigureRepository("canonical", "uploadpack.allowAnySHA1InWant", "true"); err != nil {
		t.Fatal(err)
	}
	checkout := cloneExistingCheckoutForRemoteMirrorTest(t, canonical.RepoURL("canonical"))
	mirror := copyOnHostMirrorHTTPRepo(t, canonical.RepoURL("canonical"), "mirror")
	if err := mirror.ConfigureRepository("mirror", "uploadpack.allowFilter", "true"); err != nil {
		t.Fatal(err)
	}
	commit, _, err := canonical.PushBranch("canonical", "feature-branch")
	if err != nil {
		t.Fatal(err)
	}

	e, attempt := newExistingCheckoutRemoteMirrorExecutor(t, checkout, canonical.RepoURL("canonical"), mirror.RepoURL("mirror"), commit)
	if err := e.fetchSource(t.Context(), true, &attempt); err != nil {
		t.Fatalf("fetchSource() error = %v", err)
	}
	if attempt.outcome != remoteMirrorOutcomeMiss {
		t.Errorf("outcome = %q, want miss", attempt.outcome)
	}
	assertGitConfigForRemoteMirrorTest(t, checkout, "remote.origin.promisor", "true")
	assertGitConfigForRemoteMirrorTest(t, checkout, "remote.origin.partialclonefilter", "blob:none")
	if got := gitConfigForRemoteMirrorTest(t, checkout, "remote."+attempt.url+".promisor"); got != "" {
		t.Errorf("mirror promisor remains after lag miss: %q", got)
	}
	if !hasGitCommit(t.Context(), e.shell, ".git", commit) {
		t.Error("canonical fallback did not fetch lagging commit")
	}
}

func TestPrepareRemoteMirrorFetchHonorsExplicitFilterFlags(t *testing.T) {
	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	checkout := cloneExistingCheckoutForRemoteMirrorTest(t, canonical.RepoURL("canonical"))
	runGitForMirrorTest(t, checkout, "config", "remote.origin.partialclonefilter", "blob:none")
	e, _ := newExistingCheckoutRemoteMirrorExecutor(
		t,
		checkout,
		canonical.RepoURL("canonical"),
		"https://mirror.example/repo.git",
		strings.Repeat("a", 40),
	)

	for _, flags := range []string{
		"-v --no-filter",
		"-v --no-fi",
		"-v --no-fil",
		"-v --filter=tree:0",
		"-v --fi=tree:0",
		"-v --fil=tree:0",
	} {
		got, err := e.prepareRemoteMirrorFetch(t.Context(), flags)
		if err != nil {
			t.Fatal(err)
		}
		if got != flags {
			t.Errorf("mirror fetch flags = %q, want explicit flags %q preserved", got, flags)
		}
	}
}

func TestRemoteMirrorPromisorMarkerDoesNotContainCredentials(t *testing.T) {
	mirrorURL := "https://token:secret@mirror.example/repo.git"
	marker := remoteMirrorPromisorMarker(mirrorURL)
	if strings.Contains(marker, mirrorURL) ||
		strings.Contains(marker, "token") ||
		strings.Contains(marker, "secret") {
		t.Errorf("remoteMirrorPromisorMarker() = %q, want credential-free digest", marker)
	}
	if len(marker) != sha256.Size*2 {
		t.Errorf("remoteMirrorPromisorMarker() length = %d, want %d", len(marker), sha256.Size*2)
	}
}

func TestFetchSourceRepairsPreexistingMirrorPromisorOnMiss(t *testing.T) {
	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	if err := canonical.ConfigureRepository("canonical", "uploadpack.allowFilter", "true"); err != nil {
		t.Fatal(err)
	}
	if err := canonical.ConfigureRepository("canonical", "uploadpack.allowAnySHA1InWant", "true"); err != nil {
		t.Fatal(err)
	}
	checkout := cloneExistingCheckoutForRemoteMirrorTest(t, canonical.RepoURL("canonical"))
	commit, _, err := canonical.PushBranch("canonical", "feature-branch")
	if err != nil {
		t.Fatal(err)
	}
	mirrorURL := "http://127.0.0.1:1/mirror.git"
	runGitForMirrorTest(t, checkout, "config", "core.repositoryformatversion", "1")
	runGitForMirrorTest(t, checkout, "config", "remote."+mirrorURL+".promisor", "true")
	runGitForMirrorTest(t, checkout, "config", "remote."+mirrorURL+".partialclonefilter", "blob:none")
	runGitForMirrorTest(
		t,
		checkout,
		"config",
		remoteMirrorPromisorMarkerKey,
		remoteMirrorPromisorMarker(mirrorURL),
	)
	e, attempt := newExistingCheckoutRemoteMirrorExecutor(t, checkout, canonical.RepoURL("canonical"), mirrorURL, commit)

	if err := e.fetchSource(t.Context(), true, &attempt); err != nil {
		t.Fatalf("fetchSource() error = %v", err)
	}
	assertGitConfigForRemoteMirrorTest(t, checkout, "remote.origin.promisor", "true")
	assertGitConfigForRemoteMirrorTest(t, checkout, "remote.origin.partialclonefilter", "blob:none")
	if got := gitConfigForRemoteMirrorTest(t, checkout, "remote."+mirrorURL+".promisor"); got != "" {
		t.Errorf("pre-existing mirror promisor remains after repair: %q", got)
	}
	if got := gitConfigForRemoteMirrorTest(t, checkout, remoteMirrorPromisorMarkerKey); got != "" {
		t.Errorf("remote mirror promisor marker remains after repair: %q", got)
	}
}

func TestFetchSourceMarkerFailureFallsBackToCanonical(t *testing.T) {
	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	checkout := cloneExistingCheckoutForRemoteMirrorTest(t, canonical.RepoURL("canonical"))
	commit, _, err := canonical.PushBranch("canonical", "feature-branch")
	if err != nil {
		t.Fatal(err)
	}
	e, attempt := newExistingCheckoutRemoteMirrorExecutor(
		t,
		checkout,
		canonical.RepoURL("canonical"),
		"https://127.0.0.1:1/mirror.git",
		commit,
	)
	configLock := filepath.Join(checkout, ".git", "config.lock")
	if err := os.WriteFile(configLock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(configLock) })

	if err := e.fetchSource(t.Context(), false, &attempt); err != nil {
		t.Fatalf("fetchSource() error = %v", err)
	}
	if attempt.outcome != remoteMirrorOutcomeError {
		t.Errorf("outcome = %q, want error", attempt.outcome)
	}
	if !hasGitCommit(t.Context(), e.shell, ".git", commit) {
		t.Error("canonical fallback did not fetch requested commit")
	}
}

func TestFetchSourceLeavesUnmarkedUserPromisorUntouched(t *testing.T) {
	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	checkout := cloneExistingCheckoutForRemoteMirrorTest(t, canonical.RepoURL("canonical"))
	commit := gitOutputForRemoteCheckoutTest(t, checkout, "rev-parse", "refs/remotes/origin/main")
	userURL := "https://user.example/repo.git"
	runGitForMirrorTest(t, checkout, "config", "core.repositoryformatversion", "1")
	runGitForMirrorTest(t, checkout, "config", "remote."+userURL+".promisor", "true")
	runGitForMirrorTest(t, checkout, "config", "remote."+userURL+".partialclonefilter", "blob:none")
	e, _ := newExistingCheckoutRemoteMirrorExecutor(
		t,
		checkout,
		canonical.RepoURL("canonical"),
		"",
		commit,
	)
	e.GitSkipFetchExistingCommits = true

	if err := e.fetchSource(t.Context(), false, nil); err != nil {
		t.Fatalf("fetchSource() error = %v", err)
	}
	assertGitConfigForRemoteMirrorTest(t, checkout, "remote."+userURL+".promisor", "true")
	assertGitConfigForRemoteMirrorTest(t, checkout, "remote."+userURL+".partialclonefilter", "blob:none")
	if got := gitConfigForRemoteMirrorTest(t, checkout, "remote.origin.promisor"); got != "" {
		t.Errorf("remote.origin.promisor = %q, want user promisor ownership unchanged", got)
	}
}

func TestFetchSourceLeavesSameURLUnmarkedUserPromisorUntouched(t *testing.T) {
	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	checkout := cloneExistingCheckoutForRemoteMirrorTest(t, canonical.RepoURL("canonical"))
	commit := gitOutputForRemoteCheckoutTest(t, checkout, "rev-parse", "refs/remotes/origin/main")
	mirrorURL := "https://127.0.0.1:1/mirror.git"
	runGitForMirrorTest(t, checkout, "config", "core.repositoryformatversion", "1")
	runGitForMirrorTest(t, checkout, "config", "remote."+mirrorURL+".promisor", "true")
	runGitForMirrorTest(t, checkout, "config", "remote."+mirrorURL+".partialclonefilter", "blob:none")
	runGitForMirrorTest(t, checkout, "config", "remote."+mirrorURL+".user-owned", "keep")
	e, attempt := newExistingCheckoutRemoteMirrorExecutor(
		t,
		checkout,
		canonical.RepoURL("canonical"),
		mirrorURL,
		commit,
	)

	if err := e.fetchSource(t.Context(), false, &attempt); err != nil {
		t.Fatalf("fetchSource() error = %v", err)
	}
	assertGitConfigForRemoteMirrorTest(t, checkout, "remote."+mirrorURL+".promisor", "true")
	assertGitConfigForRemoteMirrorTest(t, checkout, "remote."+mirrorURL+".partialclonefilter", "blob:none")
	assertGitConfigForRemoteMirrorTest(t, checkout, "remote."+mirrorURL+".user-owned", "keep")
	if got := gitConfigForRemoteMirrorTest(t, checkout, "remote.origin.promisor"); got != "" {
		t.Errorf("remote.origin.promisor = %q, want user promisor ownership unchanged", got)
	}
	if attempt.outcome != remoteMirrorOutcomeSkipped ||
		attempt.skipReason != remoteMirrorSkipURLConfigConflict {
		t.Errorf("remote mirror attempt = %+v, want URL config conflict skip", attempt)
	}
}

func TestRepairInterruptedRemoteMirrorPromisorAfterURLRemoval(t *testing.T) {
	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	checkout := cloneExistingCheckoutForRemoteMirrorTest(t, canonical.RepoURL("canonical"))
	oldMirrorURL := "https://old-mirror.example/repo.git"
	userURL := "https://user.example/repo.git"
	runGitForMirrorTest(t, checkout, "config", "remote."+oldMirrorURL+".promisor", "true")
	runGitForMirrorTest(t, checkout, "config", "remote."+oldMirrorURL+".partialclonefilter", "blob:none")
	runGitForMirrorTest(t, checkout, "config", "remote."+userURL+".promisor", "true")
	runGitForMirrorTest(t, checkout, "config", "remote."+userURL+".partialclonefilter", "tree:0")
	runGitForMirrorTest(
		t,
		checkout,
		"config",
		remoteMirrorPromisorMarkerKey,
		remoteMirrorPromisorMarker(oldMirrorURL),
	)
	e, _ := newExistingCheckoutRemoteMirrorExecutor(
		t,
		checkout,
		canonical.RepoURL("canonical"),
		"",
		strings.Repeat("a", 40),
	)

	if err := e.repairInterruptedRemoteMirrorPromisors(t.Context()); err != nil {
		t.Fatalf("repairInterruptedRemoteMirrorPromisors() error = %v", err)
	}
	assertGitConfigForRemoteMirrorTest(t, checkout, "remote.origin.promisor", "true")
	assertGitConfigForRemoteMirrorTest(t, checkout, "remote.origin.partialclonefilter", "blob:none")
	if got := gitConfigForRemoteMirrorTest(t, checkout, "remote."+oldMirrorURL+".promisor"); got != "" {
		t.Errorf("old mirror promisor remains after repair: %q", got)
	}
	assertGitConfigForRemoteMirrorTest(t, checkout, "remote."+userURL+".promisor", "true")
	assertGitConfigForRemoteMirrorTest(t, checkout, "remote."+userURL+".partialclonefilter", "tree:0")
	if got := gitConfigForRemoteMirrorTest(t, checkout, remoteMirrorPromisorMarkerKey); got != "" {
		t.Errorf("remote mirror promisor marker remains after repair: %q", got)
	}
}

func TestRepairInterruptedRemoteMirrorPromisorClearsMarkerWithoutPromisor(t *testing.T) {
	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	checkout := cloneExistingCheckoutForRemoteMirrorTest(t, canonical.RepoURL("canonical"))
	oldMirrorURL := "https://old-mirror.example/repo.git"
	runGitForMirrorTest(
		t,
		checkout,
		"config",
		remoteMirrorPromisorMarkerKey,
		remoteMirrorPromisorMarker(oldMirrorURL),
	)
	e, _ := newExistingCheckoutRemoteMirrorExecutor(
		t,
		checkout,
		canonical.RepoURL("canonical"),
		"",
		strings.Repeat("a", 40),
	)

	if err := e.repairInterruptedRemoteMirrorPromisors(t.Context()); err != nil {
		t.Fatalf("repairInterruptedRemoteMirrorPromisors() error = %v", err)
	}
	if got := gitConfigForRemoteMirrorTest(t, checkout, remoteMirrorPromisorMarkerKey); got != "" {
		t.Errorf("orphaned remote mirror promisor marker remains: %q", got)
	}
}

func TestRepairInterruptedRemoteMirrorPromisorSupportsGitFile(t *testing.T) {
	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	checkout := filepath.Join(t.TempDir(), "checkout")
	separateGitDir := filepath.Join(t.TempDir(), "checkout.git")
	runGitForMirrorTest(
		t,
		"",
		"clone",
		"--separate-git-dir", separateGitDir,
		canonical.RepoURL("canonical"),
		checkout,
	)
	e, _ := newExistingCheckoutRemoteMirrorExecutor(
		t,
		checkout,
		canonical.RepoURL("canonical"),
		"",
		strings.Repeat("a", 40),
	)

	if err := e.repairInterruptedRemoteMirrorPromisors(t.Context()); err != nil {
		t.Fatalf("repairInterruptedRemoteMirrorPromisors() with gitfile error = %v", err)
	}
}

func TestResolveRemoteMirrorAttemptSkipsGitFileCheckout(t *testing.T) {
	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	checkout := filepath.Join(t.TempDir(), "checkout")
	separateGitDir := filepath.Join(t.TempDir(), "checkout.git")
	runGitForMirrorTest(
		t,
		"",
		"clone",
		"--separate-git-dir", separateGitDir,
		canonical.RepoURL("canonical"),
		checkout,
	)
	e, _ := newExistingCheckoutRemoteMirrorExecutor(
		t,
		checkout,
		canonical.RepoURL("canonical"),
		"https://mirror.example/repo.git",
		strings.Repeat("a", 40),
	)
	e.shell.Env.Set("BUILDKITE_BUILD_CHECKOUT_PATH", checkout)

	attempt := e.resolveRemoteMirrorAttempt(0)
	if attempt.outcome != remoteMirrorOutcomeSkipped ||
		attempt.skipReason != remoteMirrorSkipSeparateGitDir {
		t.Errorf("resolveRemoteMirrorAttempt() = %+v, want separate-git-dir skip", attempt)
	}
}

func TestFinishRemoteMirrorPromisorHidesURLCommandsInDebug(t *testing.T) {
	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	checkout := cloneExistingCheckoutForRemoteMirrorTest(t, canonical.RepoURL("canonical"))
	mirrorURL := "https://token:secret@mirror.example/repo.git"
	runGitForMirrorTest(t, checkout, "config", "remote."+mirrorURL+".promisor", "true")
	runGitForMirrorTest(t, checkout, "config", "remote."+mirrorURL+".partialclonefilter", "blob:none")
	runGitForMirrorTest(
		t,
		checkout,
		"config",
		remoteMirrorPromisorMarkerKey,
		remoteMirrorPromisorMarker(mirrorURL),
	)

	e, _ := newExistingCheckoutRemoteMirrorExecutor(
		t,
		checkout,
		canonical.RepoURL("canonical"),
		mirrorURL,
		strings.Repeat("a", 40),
	)
	logs := &bytes.Buffer{}
	commandOutput := &process.Buffer{}
	debugShell, err := shell.New(
		shell.WithDebug(true),
		shell.WithEnv(e.shell.Env),
		shell.WithLogger(shell.NewWriterLogger(logs, false, nil)),
		shell.WithStdout(commandOutput),
		shell.WithWD(checkout),
	)
	if err != nil {
		t.Fatal(err)
	}
	e.shell = debugShell

	if err := e.finishRemoteMirrorPromisor(t.Context(), mirrorURL); err != nil {
		t.Fatalf("finishRemoteMirrorPromisor() error = %v", err)
	}
	if strings.Contains(logs.String(), mirrorURL) || strings.Contains(logs.String(), "secret") {
		t.Errorf("debug job log leaks mirror URL credentials: %q", logs.String())
	}
}

func TestSilentMirrorConfigFailureDoesNotExposeURL(t *testing.T) {
	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	checkout := cloneExistingCheckoutForRemoteMirrorTest(t, canonical.RepoURL("canonical"))
	mirrorURL := "https://token:secret@mirror.example/repo.git"
	e, _ := newExistingCheckoutRemoteMirrorExecutor(
		t,
		checkout,
		canonical.RepoURL("canonical"),
		mirrorURL,
		strings.Repeat("a", 40),
	)

	err := e.runSilentLocalGitConfig(t.Context(), "--remove-section", "remote."+mirrorURL)
	if err == nil {
		t.Fatal("runSilentLocalGitConfig() error = nil, want missing-section failure")
	}
	if strings.Contains(err.Error(), mirrorURL) || strings.Contains(err.Error(), "secret") {
		t.Errorf("silent git config error leaks mirror URL credentials: %q", err)
	}
}

func TestFetchSourceExistingCheckoutRetargetsPromisorAndMaterialisesFromCanonical(t *testing.T) {
	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	if err := canonical.ConfigureRepository("canonical", "uploadpack.allowFilter", "true"); err != nil {
		t.Fatal(err)
	}
	if err := canonical.ConfigureRepository("canonical", "uploadpack.allowAnySHA1InWant", "true"); err != nil {
		t.Fatal(err)
	}
	checkout := cloneExistingCheckoutForRemoteMirrorTest(t, canonical.RepoURL("canonical"))
	commit, _, err := canonical.PushBranch("canonical", "feature-branch")
	if err != nil {
		t.Fatal(err)
	}
	mirror := copyOnHostMirrorHTTPRepo(t, canonical.RepoURL("canonical"), "mirror")
	if err := mirror.ConfigureRepository("mirror", "uploadpack.allowFilter", "true"); err != nil {
		t.Fatal(err)
	}

	e, attempt := newExistingCheckoutRemoteMirrorExecutor(t, checkout, canonical.RepoURL("canonical"), mirror.RepoURL("mirror"), commit)
	if err := e.fetchSource(t.Context(), true, &attempt); err != nil {
		t.Fatalf("fetchSource() error = %v", err)
	}
	if attempt.outcome != remoteMirrorOutcomeHit {
		t.Errorf("outcome = %q, want hit", attempt.outcome)
	}
	assertGitConfigForRemoteMirrorTest(t, checkout, "remote.origin.promisor", "true")
	assertGitConfigForRemoteMirrorTest(t, checkout, "remote.origin.partialclonefilter", "blob:none")
	if got := gitConfigForRemoteMirrorTest(t, checkout, "remote."+attempt.url+".promisor"); got != "" {
		t.Errorf("mirror promisor remains configured: %q", got)
	}
	if got := gitConfigForRemoteMirrorTest(t, checkout, "remote."+attempt.url+".partialclonefilter"); got != "" {
		t.Errorf("mirror partial-clone filter remains configured: %q", got)
	}

	mirror.Close()
	runGitForMirrorTest(t, checkout, "checkout", "--force", commit)
	got := readRemoteMirrorTestFile(t, filepath.Join(checkout, "newfile.txt"))
	if got != "This is a new file." {
		t.Errorf("materialised file = %q, want mirror-fetched pointer resolved from canonical", got)
	}
}

func TestFetchSourceExistingPartialCheckoutInheritsOriginFilter(t *testing.T) {
	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	if err := canonical.ConfigureRepository("canonical", "uploadpack.allowFilter", "true"); err != nil {
		t.Fatal(err)
	}
	if err := canonical.ConfigureRepository("canonical", "uploadpack.allowAnySHA1InWant", "true"); err != nil {
		t.Fatal(err)
	}
	checkout := filepath.Join(t.TempDir(), "checkout")
	runGitForMirrorTest(t, "", "clone", "--filter=blob:none", canonical.RepoURL("canonical"), checkout)
	commit, _, err := canonical.PushBranch("canonical", "feature-branch")
	if err != nil {
		t.Fatal(err)
	}
	mirror := copyOnHostMirrorHTTPRepo(t, canonical.RepoURL("canonical"), "mirror")
	if err := mirror.ConfigureRepository("mirror", "uploadpack.allowFilter", "true"); err != nil {
		t.Fatal(err)
	}

	e, attempt := newExistingCheckoutRemoteMirrorExecutor(t, checkout, canonical.RepoURL("canonical"), mirror.RepoURL("mirror"), commit)
	if err := e.fetchSource(t.Context(), false, &attempt); err != nil {
		t.Fatalf("fetchSource() error = %v", err)
	}
	if attempt.outcome != remoteMirrorOutcomeHit {
		t.Errorf("outcome = %q, want hit", attempt.outcome)
	}
	assertGitConfigForRemoteMirrorTest(t, checkout, "remote.origin.partialclonefilter", "blob:none")

	mirror.Close()
	runGitForMirrorTest(t, checkout, "checkout", "--force", commit)
	if got := readRemoteMirrorTestFile(t, filepath.Join(checkout, "newfile.txt")); got != "This is a new file." {
		t.Errorf("materialised file = %q, want inherited filter to lazy-fetch from canonical", got)
	}
}

func TestFetchSourceExistingCheckoutCancellationDoesNotFallback(t *testing.T) {
	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	checkout := cloneExistingCheckoutForRemoteMirrorTest(t, canonical.RepoURL("canonical"))
	commit, _, err := canonical.PushBranch("canonical", "feature-branch")
	if err != nil {
		t.Fatal(err)
	}

	requested := make(chan struct{})
	var requestedOnce sync.Once
	hung := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		requestedOnce.Do(func() { close(requested) })
		<-req.Context().Done()
	}))
	t.Cleanup(hung.Close)

	e, attempt := newExistingCheckoutRemoteMirrorExecutor(t, checkout, canonical.RepoURL("canonical"), hung.URL+"/mirror.git", commit)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	go func() {
		select {
		case <-requested:
			cancel()
		case <-ctx.Done():
		}
	}()

	err = e.fetchSource(ctx, true, &attempt)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("fetchSource() error = %v, want context.Canceled", err)
	}
	if attempt.outcome != remoteMirrorOutcomeNotReached {
		t.Errorf("outcome = %q, want not reached", attempt.outcome)
	}
	assertGitConfigForRemoteMirrorTest(t, checkout, "remote.origin.promisor", "true")
	assertGitConfigForRemoteMirrorTest(t, checkout, "remote.origin.partialclonefilter", "blob:none")
	if got := gitConfigForRemoteMirrorTest(t, checkout, "remote."+attempt.url+".promisor"); got != "" {
		t.Errorf("mirror promisor remains after cancellation: %q", got)
	}
}

func TestCheckoutCancellationCleanupFailureRemovesCheckout(t *testing.T) {
	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	checkout := cloneExistingCheckoutForRemoteMirrorTest(t, canonical.RepoURL("canonical"))
	commit, _, err := canonical.PushBranch("canonical", "feature-branch")
	if err != nil {
		t.Fatal(err)
	}

	requested := make(chan struct{})
	var requestedOnce sync.Once
	hung := httptest.NewTLSServer(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		requestedOnce.Do(func() { close(requested) })
		<-req.Context().Done()
	}))
	t.Cleanup(hung.Close)

	e, _ := newExistingCheckoutRemoteMirrorExecutor(
		t,
		checkout,
		canonical.RepoURL("canonical"),
		hung.URL+"/mirror.git",
		commit,
	)
	e.GitFetchFlags += " --filter=blob:none"
	e.GitCleanFlags = "-fdq"
	e.shell.Env.Set("BUILDKITE_BUILD_CHECKOUT_PATH", checkout)
	e.shell.Env.Set("GIT_SSL_NO_VERIFY", "true")
	if attempt := e.resolveRemoteMirrorAttempt(0); attempt.site != remoteMirrorSiteExistingCheckout {
		t.Fatalf("resolveRemoteMirrorAttempt() = %+v, want existing checkout", attempt)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	lockResult := make(chan error, 1)
	go func() {
		select {
		case <-requested:
			lockResult <- os.WriteFile(filepath.Join(checkout, ".git", "config.lock"), nil, 0o600)
			cancel()
		case <-ctx.Done():
			lockResult <- ctx.Err()
		}
	}()

	err = e.checkout(ctx)
	if lockErr := <-lockResult; lockErr != nil {
		t.Fatalf("creating config lock: %v; checkout error: %v", lockErr, err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("checkout() error = %v, want context.Canceled", err)
	}
	if !osutil.FileExists(checkout) {
		t.Fatal("checkout directory was not recreated for teardown hooks")
	}
	if osutil.FileExists(filepath.Join(checkout, ".git")) {
		t.Fatal("unsafe Git state remains after cancellation cleanup failure")
	}
}

func TestFetchSourceExistingCheckoutMirrorFailureFallsBack(t *testing.T) {
	tests := []struct {
		name        string
		mirrorURL   func(*testing.T) string
		wantOutcome remoteMirrorOutcome
	}{
		{
			name: "transport error",
			mirrorURL: func(*testing.T) string {
				return "http://127.0.0.1:1/mirror.git"
			},
			wantOutcome: remoteMirrorOutcomeError,
		},
		{
			name: "timeout",
			mirrorURL: func(t *testing.T) string {
				oldTimeout := remoteMirrorProbeTimeout
				remoteMirrorProbeTimeout = 20 * time.Millisecond
				t.Cleanup(func() { remoteMirrorProbeTimeout = oldTimeout })
				server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
					<-req.Context().Done()
				}))
				t.Cleanup(server.Close)
				return server.URL + "/mirror.git"
			},
			wantOutcome: remoteMirrorOutcomeTimeout,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			canonical := newOnHostMirrorHTTPRepo(t, "canonical")
			if err := canonical.ConfigureRepository("canonical", "uploadpack.allowFilter", "true"); err != nil {
				t.Fatal(err)
			}
			if err := canonical.ConfigureRepository("canonical", "uploadpack.allowAnySHA1InWant", "true"); err != nil {
				t.Fatal(err)
			}
			checkout := cloneExistingCheckoutForRemoteMirrorTest(t, canonical.RepoURL("canonical"))
			commit, _, err := canonical.PushBranch("canonical", "feature-branch")
			if err != nil {
				t.Fatal(err)
			}
			e, attempt := newExistingCheckoutRemoteMirrorExecutor(
				t,
				checkout,
				canonical.RepoURL("canonical"),
				tc.mirrorURL(t),
				commit,
			)

			if err := e.fetchSource(t.Context(), true, &attempt); err != nil {
				t.Fatalf("fetchSource() error = %v", err)
			}
			if attempt.outcome != tc.wantOutcome {
				t.Errorf("outcome = %q, want %q", attempt.outcome, tc.wantOutcome)
			}
			if !hasGitCommit(t.Context(), e.shell, ".git", commit) {
				t.Error("canonical fallback did not fetch requested commit")
			}
			if got := gitConfigForRemoteMirrorTest(t, checkout, "remote."+attempt.url+".promisor"); got != "" {
				t.Errorf("mirror promisor remains after fallback: %q", got)
			}
			if got := gitConfigForRemoteMirrorTest(t, checkout, "remote."+attempt.url+".partialclonefilter"); got != "" {
				t.Errorf("mirror partial-clone filter remains after fallback: %q", got)
			}
		})
	}
}

func newExistingCheckoutRemoteMirrorExecutor(
	t *testing.T,
	checkout, canonicalURL, mirrorURL, commit string,
) (*Executor, remoteMirrorAttempt) {
	t.Helper()
	e := New(ExecutorConfig{
		Repository:         canonicalURL,
		GitRemoteMirrorURL: mirrorURL,
		Commit:             commit,
		Branch:             "feature-branch",
		PullRequest:        "false",
		GitFetchFlags:      "-v --prune",
	})
	e.shell = shell.NewTestShell(t, shell.WithSignalGracePeriod(10*time.Millisecond))
	if err := e.shell.Chdir(checkout); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if e.checkoutRoot != nil {
			_ = e.checkoutRoot.Close()
			e.checkoutRoot = nil
		}
	})
	return e, remoteMirrorAttempt{
		site: remoteMirrorSiteExistingCheckout,
		url:  mirrorURL,
	}
}

func cloneExistingCheckoutForRemoteMirrorTest(t *testing.T, repository string) string {
	t.Helper()
	checkout := filepath.Join(t.TempDir(), "checkout")
	runGitForMirrorTest(t, "", "clone", repository, checkout)
	return checkout
}

func gitOutputForRemoteCheckoutTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		if len(out) == 0 {
			return ""
		}
		t.Fatalf("git %v error = %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

func gitConfigForRemoteMirrorTest(t *testing.T, dir, key string) string {
	t.Helper()
	cmd := exec.Command("git", "config", "--local", "--get", key)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return ""
		}
		t.Fatalf("git config --get %q error = %v", key, err)
	}
	return strings.TrimSpace(string(out))
}

func assertGitConfigForRemoteMirrorTest(t *testing.T, dir, key, want string) {
	t.Helper()
	if got := gitConfigForRemoteMirrorTest(t, dir, key); got != want {
		t.Errorf("git config %s = %q, want %q", key, got, want)
	}
}

func readRemoteMirrorTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(data))
}
