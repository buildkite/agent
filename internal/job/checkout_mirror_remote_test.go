package job

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/buildkite/agent/v3/internal/job/githttptest"
	"github.com/buildkite/agent/v3/internal/osutil"
	"github.com/buildkite/agent/v3/internal/process"
	"github.com/buildkite/agent/v3/internal/shell"
)

func TestUpdateGitMirrorCreatesFromRemoteMirrorAndKeepsCanonicalOrigin(t *testing.T) {
	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	commit, _, err := canonical.PushBranch("canonical", "feature-branch")
	if err != nil {
		t.Fatal(err)
	}
	mirror := copyOnHostMirrorHTTPRepo(t, canonical.RepoURL("canonical"), "mirror")

	e := newOnHostMirrorExecutor(t, canonical.RepoURL("canonical"), commit)
	if runtime.GOOS != "windows" {
		if err := os.Chmod(e.GitMirrorsPath, os.ModeSetgid|0o775); err != nil {
			t.Fatal(err)
		}
	}
	e.CleanCheckout = true
	e.Phases = []string{"checkout", "command"}
	stagingDir := remoteMirrorStagingDir(expectedOnHostMirrorDir(e))
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stagingDir, "interrupted-clone"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	attempt := remoteMirrorAttempt{
		site: remoteMirrorSiteOnHostMirror,
		url:  mirror.RepoURL("mirror"),
	}
	canonical.Close() // Creation must source all objects from the remote mirror.

	mirrorDir, err := e.updateGitMirror(t.Context(), e.Repository, &attempt)
	if err != nil {
		t.Fatalf("updateGitMirror() error = %v", err)
	}
	if mirrorDir == expectedOnHostMirrorDir(e) {
		t.Error("clean checkout did not receive a mirror snapshot")
	}
	if got, want := gitOutputForMirrorTest(t, expectedOnHostMirrorDir(e), "config", "--get", "remote.origin.url"), e.Repository; got != want {
		t.Errorf("shared remote.origin.url = %q, want canonical %q", got, want)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(expectedOnHostMirrorDir(e))
		if err != nil {
			t.Fatal(err)
		}
		if got, want := info.Mode().Perm(), os.FileMode(0o777&^osutil.Umask); got != want {
			t.Errorf("shared mirror mode = %o, want %o", got, want)
		}
		if info.Mode()&os.ModeSetgid == 0 {
			t.Errorf("shared mirror mode = %v, want setgid preserved", info.Mode())
		}
	}
	if got, want := gitOutputForMirrorTest(t, mirrorDir, "rev-parse", commit), commit; got != want {
		t.Errorf("mirror commit = %q, want %q", got, want)
	}
	if attempt.outcome != remoteMirrorOutcomeHit {
		t.Errorf("outcome = %q, want hit", attempt.outcome)
	}
	if _, err := os.Stat(stagingDir); !os.IsNotExist(err) {
		t.Errorf("staging dir remains after publication: %v", err)
	}
}

func TestUpdateGitMirrorCreationHidesRemoteURLPromptInDebug(t *testing.T) {
	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	commit, _, err := canonical.PushBranch("canonical", "feature-branch")
	if err != nil {
		t.Fatal(err)
	}
	mirror := copyOnHostMirrorHTTPRepo(t, canonical.RepoURL("canonical"), "mirror")

	e := newOnHostMirrorExecutor(t, canonical.RepoURL("canonical"), commit)
	logs := &bytes.Buffer{}
	commandOutput := &process.Buffer{}
	debugShell, err := shell.New(
		shell.WithDebug(true),
		shell.WithEnv(e.shell.Env),
		shell.WithLogger(shell.NewWriterLogger(logs, false, nil)),
		shell.WithStdout(commandOutput),
	)
	if err != nil {
		t.Fatal(err)
	}
	e.shell = debugShell
	attempt := remoteMirrorAttempt{
		site: remoteMirrorSiteOnHostMirror,
		url:  mirror.RepoURL("mirror"),
	}

	if _, err := e.updateGitMirror(t.Context(), e.Repository, &attempt); err != nil {
		t.Fatalf("updateGitMirror() error = %v", err)
	}
	if strings.Contains(logs.String(), attempt.url) {
		t.Errorf("debug job log contains remote mirror URL prompt: %q", logs.String())
	}
}

func TestUpdateGitMirrorCreationFallsBackToCanonical(t *testing.T) {
	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	commit, _, err := canonical.PushBranch("canonical", "feature-branch")
	if err != nil {
		t.Fatal(err)
	}

	e := newOnHostMirrorExecutor(t, canonical.RepoURL("canonical"), commit)
	attempt := remoteMirrorAttempt{
		site: remoteMirrorSiteOnHostMirror,
		url:  "http://127.0.0.1:1/mirror.git",
	}

	mirrorDir, err := e.updateGitMirror(t.Context(), e.Repository, &attempt)
	if err != nil {
		t.Fatalf("updateGitMirror() error = %v", err)
	}
	if got, want := gitOutputForMirrorTest(t, mirrorDir, "config", "--get", "remote.origin.url"), e.Repository; got != want {
		t.Errorf("remote.origin.url = %q, want canonical %q", got, want)
	}
	if attempt.outcome != remoteMirrorOutcomeError {
		t.Errorf("outcome = %q, want error", attempt.outcome)
	}
}

func TestUpdateGitMirrorCreationClassifiesStalledMirrorAsTimeout(t *testing.T) {
	oldLowSpeedTime := remoteMirrorLowSpeedTime
	remoteMirrorLowSpeedTime = 1
	t.Cleanup(func() { remoteMirrorLowSpeedTime = oldLowSpeedTime })

	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	commit, _, err := canonical.PushBranch("canonical", "feature-branch")
	if err != nil {
		t.Fatal(err)
	}
	stalled := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		<-req.Context().Done()
	}))
	t.Cleanup(stalled.Close)

	e := newOnHostMirrorExecutor(t, canonical.RepoURL("canonical"), commit)
	attempt := remoteMirrorAttempt{
		site: remoteMirrorSiteOnHostMirror,
		url:  stalled.URL + "/mirror.git",
	}

	if _, err := e.updateGitMirror(t.Context(), e.Repository, &attempt); err != nil {
		t.Fatalf("updateGitMirror() error = %v", err)
	}
	if attempt.outcome != remoteMirrorOutcomeTimeout {
		t.Errorf("outcome = %q, want timeout", attempt.outcome)
	}
}

func TestUpdateGitMirrorKeepsUsefulLaggingCreationClone(t *testing.T) {
	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	remoteMirror := copyOnHostMirrorHTTPRepo(t, canonical.RepoURL("canonical"), "mirror")
	commit, _, err := canonical.PushBranch("canonical", "feature-branch")
	if err != nil {
		t.Fatal(err)
	}

	e := newOnHostMirrorExecutor(t, canonical.RepoURL("canonical"), commit)
	attempt := remoteMirrorAttempt{
		site: remoteMirrorSiteOnHostMirror,
		url:  remoteMirror.RepoURL("mirror"),
	}

	mirrorDir, err := e.updateGitMirror(t.Context(), e.Repository, &attempt)
	if err != nil {
		t.Fatalf("updateGitMirror() error = %v", err)
	}
	if attempt.outcome != remoteMirrorOutcomeMiss {
		t.Errorf("outcome = %q, want miss", attempt.outcome)
	}
	if hasGitCommit(t.Context(), e.shell, mirrorDir, commit) {
		t.Error("lagging creation clone unexpectedly contains requested commit")
	}
	if got, want := gitOutputForMirrorTest(t, mirrorDir, "config", "--get", "remote.origin.url"), e.Repository; got != want {
		t.Errorf("remote.origin.url = %q, want canonical %q", got, want)
	}
	if !gitRefExistsForMirrorTest(mirrorDir, "refs/heads/main") {
		t.Error("lagging creation clone discarded useful main branch objects")
	}
}

func TestUpdateGitMirrorUpdateHitUsesNamespacedRefWithoutMovingHeadsOrTags(t *testing.T) {
	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	e := newOnHostMirrorExecutor(t, canonical.RepoURL("canonical"), "")
	mirrorDir := expectedOnHostMirrorDir(e)
	cloneOnHostMirrorToPath(t, canonical.RepoURL("canonical"), mirrorDir)

	commit, _, err := canonical.PushBranch("canonical", "feature-branch")
	if err != nil {
		t.Fatal(err)
	}
	remoteMirror := copyOnHostMirrorHTTPRepo(t, canonical.RepoURL("canonical"), "mirror")
	if _, err := remoteMirror.CreateRef("mirror", "refs/tags/lagged-tag", commit); err != nil {
		t.Fatal(err)
	}

	e.Commit = commit
	attempt := remoteMirrorAttempt{
		site: remoteMirrorSiteOnHostMirror,
		url:  remoteMirror.RepoURL("mirror"),
	}
	gotDir, err := e.updateGitMirror(t.Context(), e.Repository, &attempt)
	if err != nil {
		t.Fatalf("updateGitMirror() error = %v", err)
	}
	if gotDir != mirrorDir {
		t.Errorf("mirror dir = %q, want %q", gotDir, mirrorDir)
	}
	if attempt.outcome != remoteMirrorOutcomeHit {
		t.Errorf("outcome = %q, want hit", attempt.outcome)
	}
	_, namespacedRef, _ := strings.Cut(remoteMirrorOnHostRefspec(commit, e.Branch), ":")
	if got, want := gitOutputForMirrorTest(t, mirrorDir, "rev-parse", namespacedRef), commit; got != want {
		t.Errorf("namespaced ref = %q, want %q", got, want)
	}
	if gitRefExistsForMirrorTest(mirrorDir, "refs/heads/feature-branch") {
		t.Error("remote mirror update wrote refs/heads/feature-branch")
	}
	if gitRefExistsForMirrorTest(mirrorDir, "refs/tags/lagged-tag") {
		t.Error("remote mirror update auto-followed a lagged tag despite --no-tags")
	}

	checkout := filepath.Join(t.TempDir(), "checkout")
	runGitForMirrorTest(t, "", "clone", "--reference", mirrorDir, canonical.RepoURL("canonical"), checkout)
	countObjects := gitOutputForMirrorTest(t, filepath.Join(checkout, ".git"), "count-objects", "-v")
	if !strings.Contains(countObjects, "count: 0") || !strings.Contains(countObjects, "in-pack: 0") {
		t.Errorf("reference clone copied objects from canonical:\n%s", countObjects)
	}
}

func TestUpdateGitMirrorUpdateHitRepairsCollidingCanonicalOrigin(t *testing.T) {
	canonical := newOnHostMirrorHTTPRepo(t, "canonical.one")
	if err := canonical.CreateRepository("canonical-one"); err != nil {
		t.Fatal(err)
	}
	if out, err := canonical.InitRepository("canonical-one"); err != nil {
		t.Fatalf("InitRepository() error = %v\n%s", err, out)
	}
	oldURL := canonical.RepoURL("canonical.one")
	newURL := canonical.RepoURL("canonical-one")
	if dirForRepository(oldURL) != dirForRepository(newURL) {
		t.Fatalf("test URLs do not collide: %q and %q", oldURL, newURL)
	}

	newCommit, _, err := canonical.PushBranch("canonical-one", "feature-branch")
	if err != nil {
		t.Fatal(err)
	}

	e := newOnHostMirrorExecutor(t, newURL, newCommit)
	mirrorDir := expectedOnHostMirrorDir(e)
	cloneOnHostMirrorToPath(t, oldURL, mirrorDir)
	remoteMirror := copyOnHostMirrorHTTPRepo(t, newURL, "mirror")
	attempt := remoteMirrorAttempt{
		site: remoteMirrorSiteOnHostMirror,
		url:  remoteMirror.RepoURL("mirror"),
	}

	if _, err := e.updateGitMirror(t.Context(), e.Repository, &attempt); err != nil {
		t.Fatalf("updateGitMirror() error = %v", err)
	}
	if attempt.outcome != remoteMirrorOutcomeHit {
		t.Fatalf("outcome = %q, want hit", attempt.outcome)
	}
	if got := gitOutputForMirrorTest(t, mirrorDir, "config", "--get", "remote.origin.url"); got != newURL {
		t.Errorf("remote.origin.url = %q, want repaired canonical %q", got, newURL)
	}
}

func TestUpdateGitMirrorWarmHitRepairsCollidingCanonicalOrigin(t *testing.T) {
	canonical := newOnHostMirrorHTTPRepo(t, "canonical.one")
	if err := canonical.CreateRepository("canonical-one"); err != nil {
		t.Fatal(err)
	}
	oldURL := canonical.RepoURL("canonical.one")
	newURL := canonical.RepoURL("canonical-one")
	if dirForRepository(oldURL) != dirForRepository(newURL) {
		t.Fatalf("test URLs do not collide: %q and %q", oldURL, newURL)
	}

	e := newOnHostMirrorExecutor(t, newURL, "")
	mirrorDir := expectedOnHostMirrorDir(e)
	cloneOnHostMirrorToPath(t, oldURL, mirrorDir)
	runGitForMirrorTest(t, mirrorDir, "push", "--mirror", newURL)
	e.Commit = gitOutputForMirrorTest(t, mirrorDir, "rev-parse", "refs/heads/main")
	attempt := remoteMirrorAttempt{
		site: remoteMirrorSiteOnHostMirror,
		url:  "https://mirror.example/repo.git",
	}

	if _, err := e.updateGitMirror(t.Context(), e.Repository, &attempt); err != nil {
		t.Fatalf("updateGitMirror() error = %v", err)
	}
	if attempt.outcome != remoteMirrorOutcomeNotReached {
		t.Fatalf("outcome = %q, want notReached warm hit", attempt.outcome)
	}
	if got := gitOutputForMirrorTest(t, mirrorDir, "config", "--get", "remote.origin.url"); got != newURL {
		t.Errorf("remote.origin.url = %q, want repaired canonical %q", got, newURL)
	}
}

func TestUpdateGitMirrorNoRemoteURLUsesUnpinnedWarmCommit(t *testing.T) {
	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	e := newOnHostMirrorExecutor(t, canonical.RepoURL("canonical"), "")
	mirrorDir := expectedOnHostMirrorDir(e)
	cloneOnHostMirrorToPath(t, e.Repository, mirrorDir)

	parent := gitOutputForMirrorTest(t, mirrorDir, "rev-parse", "refs/heads/main")
	tree := gitOutputForMirrorTest(t, mirrorDir, "rev-parse", "refs/heads/main^{tree}")
	commit := gitOutputForMirrorTest(
		t,
		mirrorDir,
		"commit-tree", tree,
		"-p", parent,
		"-m", "Unpinned warm commit",
	)
	if !hasGitCommit(t.Context(), e.shell, mirrorDir, commit) {
		t.Fatal("test commit is absent from mirror")
	}
	if hasGitCommitReachableFromRef(t.Context(), e.shell, mirrorDir, commit) {
		t.Fatal("test commit unexpectedly reachable from a ref")
	}

	var commandLog [][]string
	e.shell = shell.NewTestShell(
		t,
		shell.WithSignalGracePeriod(10*time.Millisecond),
		shell.WithCommandLog(&commandLog),
	)
	e.Commit = commit
	attempt := e.resolveRemoteMirrorAttempt(0)
	if attempt.outcome != remoteMirrorOutcomeSkipped ||
		attempt.skipReason != remoteMirrorSkipNoURL {
		t.Fatalf("attempt = %+v, want no-url skip", attempt)
	}
	canonical.Close()

	gotDir, err := e.updateGitMirror(t.Context(), e.Repository, &attempt)
	if err != nil {
		t.Fatalf("updateGitMirror() error = %v, want warm mirror fast path", err)
	}
	if gotDir != mirrorDir {
		t.Errorf("mirror dir = %q, want %q", gotDir, mirrorDir)
	}
	for _, command := range commandLog {
		if slices.Contains(command, "for-each-ref") {
			t.Errorf("warm mirror fast path ran global reachability scan: %q", command)
		}
	}
}

func TestGetOrUpdateMirrorDirCloneLockTimeoutFallsBackWithoutMirror(t *testing.T) {
	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	commit, _, err := canonical.PushBranch("canonical", "feature-branch")
	if err != nil {
		t.Fatal(err)
	}
	e := newOnHostMirrorExecutor(t, canonical.RepoURL("canonical"), commit)
	e.GitMirrorsLockTimeout = 0
	lock, err := e.shell.LockFile(t.Context(), expectedOnHostMirrorDir(e)+".clonelock")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lock.Unlock() })
	attempt := remoteMirrorAttempt{
		site: remoteMirrorSiteOnHostMirror,
		url:  "https://mirror.example/repo.git",
	}

	mirrorDir, err := e.getOrUpdateMirrorDir(t.Context(), e.Repository, &attempt)
	if err != nil {
		t.Fatalf("getOrUpdateMirrorDir() error = %v, want canonical checkout fallback", err)
	}
	if mirrorDir != "" {
		t.Errorf("mirrorDir = %q, want no on-host reference after lock timeout", mirrorDir)
	}
	if attempt.outcome != remoteMirrorOutcomeTimeout {
		t.Errorf("outcome = %q, want timeout", attempt.outcome)
	}
}

func TestUpdateGitMirrorUpdateMissFallsBackToCanonical(t *testing.T) {
	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	remoteMirror := copyOnHostMirrorHTTPRepo(t, canonical.RepoURL("canonical"), "mirror")

	e := newOnHostMirrorExecutor(t, canonical.RepoURL("canonical"), "")
	mirrorDir := expectedOnHostMirrorDir(e)
	cloneOnHostMirrorToPath(t, canonical.RepoURL("canonical"), mirrorDir)

	commit, _, err := canonical.PushBranch("canonical", "feature-branch")
	if err != nil {
		t.Fatal(err)
	}
	e.Commit = commit
	attempt := remoteMirrorAttempt{
		site: remoteMirrorSiteOnHostMirror,
		url:  remoteMirror.RepoURL("mirror"),
	}

	if _, err := e.updateGitMirror(t.Context(), e.Repository, &attempt); err != nil {
		t.Fatalf("updateGitMirror() error = %v", err)
	}
	if attempt.outcome != remoteMirrorOutcomeMiss {
		t.Errorf("outcome = %q, want miss", attempt.outcome)
	}
	if got, want := gitOutputForMirrorTest(t, mirrorDir, "rev-parse", commit), commit; got != want {
		t.Errorf("canonical fallback commit = %q, want %q", got, want)
	}
	if got, want := gitOutputForMirrorTest(t, mirrorDir, "rev-parse", "refs/heads/feature-branch"), commit; got != want {
		t.Errorf("canonical branch ref = %q, want %q", got, want)
	}
}

func TestUpdateGitMirrorRefWriteFailureDoesNotCountAsHit(t *testing.T) {
	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	e := newOnHostMirrorExecutor(t, canonical.RepoURL("canonical"), "")
	mirrorDir := expectedOnHostMirrorDir(e)
	cloneOnHostMirrorToPath(t, canonical.RepoURL("canonical"), mirrorDir)

	commit, _, err := canonical.PushBranch("canonical", "feature-branch")
	if err != nil {
		t.Fatal(err)
	}
	remoteMirror := copyOnHostMirrorHTTPRepo(t, canonical.RepoURL("canonical"), "mirror")
	conflictParent := filepath.Join(mirrorDir, "refs", "buildkite-agent")
	if err := os.MkdirAll(conflictParent, 0o755); err != nil {
		t.Fatal(err)
	}
	existingCommit := gitOutputForMirrorTest(t, mirrorDir, "rev-parse", "refs/heads/main")
	if err := os.WriteFile(filepath.Join(conflictParent, "remote-mirror"), []byte(existingCommit+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	e.Commit = commit
	attempt := remoteMirrorAttempt{
		site: remoteMirrorSiteOnHostMirror,
		url:  remoteMirror.RepoURL("mirror"),
	}
	if _, err := e.updateGitMirror(t.Context(), e.Repository, &attempt); err != nil {
		t.Fatalf("updateGitMirror() error = %v", err)
	}
	if attempt.outcome == remoteMirrorOutcomeHit {
		t.Fatal("ref-write failure counted as a remote mirror hit")
	}
	if got, want := gitOutputForMirrorTest(t, mirrorDir, "rev-parse", "refs/heads/feature-branch"), commit; got != want {
		t.Errorf("canonical fallback branch = %q, want %q", got, want)
	}
}

func TestUpdateGitMirrorRetryDoesNotTrustUnpinnedCommit(t *testing.T) {
	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	canonicalURL, err := url.Parse(canonical.URL)
	if err != nil {
		t.Fatal(err)
	}
	canonicalProxy := httputil.NewSingleHostReverseProxy(canonicalURL)
	var canonicalAvailable atomic.Bool
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if !canonicalAvailable.Load() {
			http.Error(w, "canonical unavailable", http.StatusServiceUnavailable)
			return
		}
		canonicalProxy.ServeHTTP(w, req)
	}))
	t.Cleanup(proxy.Close)

	e := newOnHostMirrorExecutor(t, proxy.URL+"/canonical.git", "")
	mirrorDir := expectedOnHostMirrorDir(e)
	cloneOnHostMirrorToPath(t, canonical.RepoURL("canonical"), mirrorDir)

	mainRef := exec.Command("git", "ls-remote", canonical.RepoURL("canonical"), "refs/heads/main")
	mainOutput, err := mainRef.Output()
	if err != nil {
		t.Fatal(err)
	}
	mainCommit := strings.Fields(string(mainOutput))[0]

	commit, _, err := canonical.PushBranch("canonical", "feature-branch")
	if err != nil {
		t.Fatal(err)
	}
	remoteMirror := copyOnHostMirrorHTTPRepo(t, canonical.RepoURL("canonical"), "mirror")
	e.GitRemoteMirrorURL = remoteMirror.RepoURL("mirror")
	if _, err := canonical.CreateRef("canonical", "refs/heads/feature-branch", mainCommit); err != nil {
		t.Fatal(err)
	}
	conflict := filepath.Join(mirrorDir, "refs", "buildkite-agent", "remote-mirror")
	if err := os.MkdirAll(filepath.Dir(conflict), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(conflict, []byte(commit+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	e.Commit = commit
	attempt := remoteMirrorAttempt{site: remoteMirrorSiteOnHostMirror, url: remoteMirror.RepoURL("mirror")}
	if _, err := e.updateGitMirror(t.Context(), e.Repository, &attempt); err == nil {
		t.Fatal("updateGitMirror() error = nil, want canonical fallback failure")
	}
	if !hasGitCommit(t.Context(), e.shell, mirrorDir, commit) {
		t.Fatal("failed destination ref did not leave the fetched object for retry regression")
	}
	if err := os.Remove(conflict); err != nil {
		t.Fatal(err)
	}

	canonicalAvailable.Store(true)
	checkoutDir := t.TempDir()
	runGitForMirrorTest(t, "", "clone", "--reference", mirrorDir, "--", e.Repository, checkoutDir)
	alternates := filepath.Join(checkoutDir, ".git", "objects", "info", "alternates")
	if !osutil.FileExists(alternates) {
		t.Fatal("reference checkout has no alternates file")
	}
	e.shell.Env.Set("BUILDKITE_BUILD_CHECKOUT_PATH", checkoutDir)
	e.GitMirrorCheckoutMode = "reference"
	e.GitSkipFetchExistingCommits = true

	if err := e.defaultCheckoutPhase(t.Context(), 1); err == nil {
		t.Fatal("defaultCheckoutPhase() retry error = nil, want canonical fetch failure for force-pushed commit")
	}
	if osutil.FileExists(alternates) {
		t.Fatal("unsafe checkout alternate remains after mirror bypass")
	}
}

func TestUpdateGitMirrorExistingCommitDoesNotContactRemoteMirror(t *testing.T) {
	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	commit, _, err := canonical.PushBranch("canonical", "feature-branch")
	if err != nil {
		t.Fatal(err)
	}
	e := newOnHostMirrorExecutor(t, canonical.RepoURL("canonical"), commit)
	mirrorDir := expectedOnHostMirrorDir(e)
	cloneOnHostMirrorToPath(t, canonical.RepoURL("canonical"), mirrorDir)
	stagingDir := remoteMirrorStagingDir(mirrorDir)
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stagingDir, "interrupted-clone"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	attempt := remoteMirrorAttempt{
		site: remoteMirrorSiteOnHostMirror,
		url:  "http://127.0.0.1:1/mirror.git",
	}

	if _, err := e.updateGitMirror(t.Context(), e.Repository, &attempt); err != nil {
		t.Fatalf("updateGitMirror() error = %v", err)
	}
	if attempt.outcome != remoteMirrorOutcomeNotReached {
		t.Errorf("outcome = %q, want not reached", attempt.outcome)
	}
	if _, err := os.Stat(stagingDir); !os.IsNotExist(err) {
		t.Errorf("stale staging dir remains beside valid mirror: %v", err)
	}
}

func TestUpdateGitMirrorReclaimsStagingWithoutRemoteAttempt(t *testing.T) {
	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	commit, _, err := canonical.PushBranch("canonical", "feature-branch")
	if err != nil {
		t.Fatal(err)
	}
	e := newOnHostMirrorExecutor(t, canonical.RepoURL("canonical"), commit)
	mirrorDir := expectedOnHostMirrorDir(e)
	cloneOnHostMirrorToPath(t, canonical.RepoURL("canonical"), mirrorDir)
	stagingDir := remoteMirrorStagingDir(mirrorDir)
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stagingDir, "interrupted-clone"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := e.updateGitMirror(t.Context(), e.Repository, nil); err != nil {
		t.Fatalf("updateGitMirror() error = %v", err)
	}
	if _, err := os.Stat(stagingDir); !os.IsNotExist(err) {
		t.Errorf("stale staging dir remains without remote attempt: %v", err)
	}
}

func TestUpdateGitMirrorCancellationDoesNotFallbackToCanonical(t *testing.T) {
	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
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

	e := newOnHostMirrorExecutor(t, canonical.RepoURL("canonical"), commit)
	attempt := remoteMirrorAttempt{
		site: remoteMirrorSiteOnHostMirror,
		url:  hung.URL + "/mirror.git",
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	go func() {
		select {
		case <-requested:
			cancel()
		case <-ctx.Done():
		}
	}()

	_, err = e.updateGitMirror(ctx, e.Repository, &attempt)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("updateGitMirror() error = %v, want context.Canceled", err)
	}
	if attempt.outcome != remoteMirrorOutcomeNotReached {
		t.Errorf("outcome = %q, want not reached", attempt.outcome)
	}
	if osutil.FileExists(expectedOnHostMirrorDir(e)) {
		t.Error("cancelled remote mirror clone fell back to canonical")
	}
}

func newOnHostMirrorExecutor(t *testing.T, repository, commit string) *Executor {
	t.Helper()
	e := New(ExecutorConfig{
		Repository:            repository,
		Commit:                commit,
		Branch:                "feature-branch",
		PullRequest:           "false",
		GitMirrorsPath:        t.TempDir(),
		GitMirrorsLockTimeout: 30,
		GitCloneMirrorFlags:   "-v",
	})
	e.shell = shell.NewTestShell(t, shell.WithSignalGracePeriod(10*time.Millisecond))
	t.Cleanup(func() {
		if e.checkoutRoot != nil {
			_ = e.checkoutRoot.Close()
			e.checkoutRoot = nil
		}
	})
	return e
}

func newOnHostMirrorHTTPRepo(t *testing.T, name string) *githttptest.Server {
	t.Helper()
	t.Setenv("GIT_AUTHOR_NAME", "Buildkite Agent")
	t.Setenv("GIT_AUTHOR_EMAIL", "agent@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Buildkite Agent")
	t.Setenv("GIT_COMMITTER_EMAIL", "agent@example.com")

	server := githttptest.NewServer()
	t.Cleanup(server.Close)
	if err := server.CreateRepository(name); err != nil {
		t.Fatal(err)
	}
	if out, err := server.InitRepository(name); err != nil {
		t.Fatalf("InitRepository() error = %v\n%s", err, out)
	}
	return server
}

func copyOnHostMirrorHTTPRepo(t *testing.T, sourceURL, name string) *githttptest.Server {
	t.Helper()
	server := githttptest.NewServer()
	t.Cleanup(server.Close)
	if err := server.CreateRepository(name); err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	runGitForMirrorTest(t, "", "clone", "--mirror", sourceURL, tmp)
	runGitForMirrorTest(t, tmp, "push", "--mirror", server.RepoURL(name))
	return server
}

func cloneOnHostMirrorToPath(t *testing.T, sourceURL, path string) {
	t.Helper()
	runGitForMirrorTest(t, "", "clone", "--mirror", sourceURL, path)
}

func expectedOnHostMirrorDir(e *Executor) string {
	return filepath.Join(e.GitMirrorsPath, dirForRepository(e.Repository))
}

func runGitForMirrorTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v error = %v\n%s", args, err, out)
	}
}

func gitOutputForMirrorTest(t *testing.T, gitDir string, args ...string) string {
	t.Helper()
	commandArgs := append([]string{"--git-dir", gitDir}, args...)
	cmd := exec.Command("git", commandArgs...)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v error = %v", commandArgs, err)
	}
	return strings.TrimSpace(string(out))
}

func gitRefExistsForMirrorTest(gitDir, ref string) bool {
	cmd := exec.Command("git", "--git-dir", gitDir, "show-ref", "--verify", "--quiet", ref)
	return cmd.Run() == nil
}

func TestRemoteMirrorOnHostRefspec(t *testing.T) {
	t.Parallel()
	commit := strings.Repeat("a", 40)
	got := remoteMirrorOnHostRefspec(commit, "feature/foo")
	want := "+" + commit + ":refs/buildkite-agent/remote-mirror/feature-foo"
	if got != want {
		t.Errorf("remoteMirrorOnHostRefspec() = %q, want %q", got, want)
	}
}
