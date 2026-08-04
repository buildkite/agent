package job

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/buildkite/agent/v3/internal/osutil"
	"github.com/buildkite/agent/v3/internal/process"
	"github.com/buildkite/agent/v3/internal/shell"
	"github.com/buildkite/shellwords"
)

func TestPrepareCheckoutWorkdirRemoteMirrorHitSkipsCanonicalFetch(t *testing.T) {
	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	commit, _, err := canonical.PushBranch("canonical", "feature-branch")
	if err != nil {
		t.Fatal(err)
	}
	mirror := copyOnHostMirrorHTTPRepo(t, canonical.RepoURL("canonical"), "mirror")
	e, attempt := newFreshCloneRemoteMirrorExecutor(t, canonical.RepoURL("canonical"), mirror.RepoURL("mirror"), commit)
	canonical.Close()

	if err := e.prepareCheckoutWorkdir(t.Context(), &attempt, sparseCheckout{}, "", []string{"-v"}, false); err != nil {
		t.Fatalf("prepareCheckoutWorkdir() error = %v", err)
	}
	if attempt.outcome != remoteMirrorOutcomeHit {
		t.Errorf("outcome = %q, want hit", attempt.outcome)
	}
	assertGitConfigForRemoteMirrorTest(t, e.shell.Getwd(), "remote.origin.url", e.Repository)
	if err := e.fetchSource(t.Context(), false, &attempt); err != nil {
		t.Fatalf("fetchSource() after hit error = %v", err)
	}
}

func TestPrepareCheckoutWorkdirKeepsLaggingMirrorClone(t *testing.T) {
	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	mirror := copyOnHostMirrorHTTPRepo(t, canonical.RepoURL("canonical"), "mirror")
	commit, _, err := canonical.PushBranch("canonical", "feature-branch")
	if err != nil {
		t.Fatal(err)
	}
	e, attempt := newFreshCloneRemoteMirrorExecutor(t, canonical.RepoURL("canonical"), mirror.RepoURL("mirror"), commit)

	if err := e.prepareCheckoutWorkdir(t.Context(), &attempt, sparseCheckout{}, "", []string{"-v"}, false); err != nil {
		t.Fatalf("prepareCheckoutWorkdir() error = %v", err)
	}
	if attempt.outcome != remoteMirrorOutcomeMiss {
		t.Errorf("outcome = %q, want miss", attempt.outcome)
	}
	if _, err := os.Stat(filepath.Join(e.shell.Getwd(), ".git")); err != nil {
		t.Errorf("useful lagging mirror clone was discarded: %v", err)
	}
	if err := e.fetchSource(t.Context(), false, &attempt); err != nil {
		t.Fatalf("canonical delta fetch error = %v", err)
	}
	if !hasGitCommit(t.Context(), e.shell, ".git", commit) {
		t.Error("canonical delta fetch did not add requested commit")
	}
}

func TestPrepareCheckoutWorkdirReclonesLaggingShallowMirror(t *testing.T) {
	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	if err := canonical.SetDefaultBranch("canonical", "main"); err != nil {
		t.Fatal(err)
	}
	mirror := copyOnHostMirrorHTTPRepo(t, canonical.RepoURL("canonical"), "mirror")
	if err := mirror.SetDefaultBranch("mirror", "main"); err != nil {
		t.Fatal(err)
	}

	source := filepath.Join(t.TempDir(), "source")
	runGitForMirrorTest(t, "", "clone", canonical.RepoURL("canonical"), source)
	if err := os.WriteFile(filepath.Join(source, "second.txt"), []byte("second\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitForMirrorTest(t, source, "add", "second.txt")
	runGitForMirrorTest(t, source, "commit", "-m", "Advance main")
	runGitForMirrorTest(t, source, "push", "origin", "main")
	commit := gitOutputForRemoteCheckoutTest(t, source, "rev-parse", "HEAD")

	e, attempt := newFreshCloneRemoteMirrorExecutor(t, canonical.RepoURL("canonical"), mirror.RepoURL("mirror"), commit)
	if err := e.prepareCheckoutWorkdir(
		t.Context(),
		&attempt,
		sparseCheckout{},
		"",
		[]string{"--dept=1"},
		false,
	); err != nil {
		t.Fatalf("prepareCheckoutWorkdir() error = %v", err)
	}
	if attempt.outcome != remoteMirrorOutcomeMiss {
		t.Fatalf("outcome = %q, want lag miss", attempt.outcome)
	}
	if err := e.fetchSource(t.Context(), false, &attempt); err != nil {
		t.Fatalf("fetchSource() error = %v", err)
	}

	control := filepath.Join(t.TempDir(), "canonical-control")
	runGitForMirrorTest(t, "", "clone", "--depth=1", canonical.RepoURL("canonical"), control)
	gotShallow := readRemoteMirrorTestFile(t, filepath.Join(e.shell.Getwd(), ".git", "shallow"))
	wantShallow := readRemoteMirrorTestFile(t, filepath.Join(control, ".git", "shallow"))
	if gotShallow != wantShallow {
		t.Errorf("shallow boundary = %q, want canonical boundary %q", gotShallow, wantShallow)
	}
	gotCount := gitOutputForRemoteCheckoutTest(t, e.shell.Getwd(), "rev-list", "--count", commit)
	wantCount := gitOutputForRemoteCheckoutTest(t, control, "rev-list", "--count", commit)
	if gotCount != wantCount {
		t.Errorf("reachable commit count = %s, want canonical shallow count %s", gotCount, wantCount)
	}
}

func TestPrepareCheckoutWorkdirReclonesShallowReferenceFalseHit(t *testing.T) {
	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	if err := canonical.SetDefaultBranch("canonical", "main"); err != nil {
		t.Fatal(err)
	}
	mirror := copyOnHostMirrorHTTPRepo(t, canonical.RepoURL("canonical"), "mirror")
	if err := mirror.SetDefaultBranch("mirror", "main"); err != nil {
		t.Fatal(err)
	}

	source := filepath.Join(t.TempDir(), "source")
	runGitForMirrorTest(t, "", "clone", canonical.RepoURL("canonical"), source)
	if err := os.WriteFile(filepath.Join(source, "second.txt"), []byte("second\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitForMirrorTest(t, source, "add", "second.txt")
	runGitForMirrorTest(t, source, "commit", "-m", "Advance main")
	runGitForMirrorTest(t, source, "push", "origin", "main")
	commit := gitOutputForRemoteCheckoutTest(t, source, "rev-parse", "HEAD")

	reference := filepath.Join(t.TempDir(), "reference.git")
	cloneOnHostMirrorToPath(t, canonical.RepoURL("canonical"), reference)
	flags := []string{"--depth=1", "--reference", reference}
	e, attempt := newFreshCloneRemoteMirrorExecutor(t, canonical.RepoURL("canonical"), mirror.RepoURL("mirror"), commit)
	if err := e.prepareCheckoutWorkdir(t.Context(), &attempt, sparseCheckout{}, "", flags, false); err != nil {
		t.Fatalf("prepareCheckoutWorkdir() error = %v", err)
	}
	if attempt.outcome != remoteMirrorOutcomeMiss {
		t.Fatalf("outcome = %q, want ambiguous shallow reference treated as miss", attempt.outcome)
	}
	if err := e.fetchSource(t.Context(), false, &attempt); err != nil {
		t.Fatalf("fetchSource() error = %v", err)
	}

	control := filepath.Join(t.TempDir(), "canonical-control")
	runGitForMirrorTest(t, "", "clone", "--depth=1", "--reference", reference, canonical.RepoURL("canonical"), control)
	gotShallow := readRemoteMirrorTestFile(t, filepath.Join(e.shell.Getwd(), ".git", "shallow"))
	wantShallow := readRemoteMirrorTestFile(t, filepath.Join(control, ".git", "shallow"))
	if gotShallow != wantShallow {
		t.Errorf("shallow boundary = %q, want canonical boundary %q", gotShallow, wantShallow)
	}
}

func TestPrepareCheckoutWorkdirReclonesShallowAmbientAlternateFalseHit(t *testing.T) {
	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	if err := canonical.SetDefaultBranch("canonical", "main"); err != nil {
		t.Fatal(err)
	}
	mirror := copyOnHostMirrorHTTPRepo(t, canonical.RepoURL("canonical"), "mirror")
	if err := mirror.SetDefaultBranch("mirror", "main"); err != nil {
		t.Fatal(err)
	}

	source := filepath.Join(t.TempDir(), "source")
	runGitForMirrorTest(t, "", "clone", canonical.RepoURL("canonical"), source)
	if err := os.WriteFile(filepath.Join(source, "second.txt"), []byte("second\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitForMirrorTest(t, source, "add", "second.txt")
	runGitForMirrorTest(t, source, "commit", "-m", "Advance main")
	runGitForMirrorTest(t, source, "push", "origin", "main")
	commit := gitOutputForRemoteCheckoutTest(t, source, "rev-parse", "HEAD")

	reference := filepath.Join(t.TempDir(), "reference.git")
	cloneOnHostMirrorToPath(t, canonical.RepoURL("canonical"), reference)
	e, attempt := newFreshCloneRemoteMirrorExecutor(t, canonical.RepoURL("canonical"), mirror.RepoURL("mirror"), commit)
	e.shell.Env.Set("GIT_ALTERNATE_OBJECT_DIRECTORIES", filepath.Join(reference, "objects"))
	if err := e.prepareCheckoutWorkdir(
		t.Context(),
		&attempt,
		sparseCheckout{},
		"",
		[]string{"--depth=1"},
		false,
	); err != nil {
		t.Fatalf("prepareCheckoutWorkdir() error = %v", err)
	}
	if attempt.outcome != remoteMirrorOutcomeMiss {
		t.Fatalf("outcome = %q, want ambient alternate shallow hit treated as miss", attempt.outcome)
	}
	if err := e.fetchSource(t.Context(), false, &attempt); err != nil {
		t.Fatalf("fetchSource() error = %v", err)
	}

	control := filepath.Join(t.TempDir(), "canonical-control")
	runGitForMirrorTest(t, "", "clone", "--depth=1", canonical.RepoURL("canonical"), control)
	gotShallow := readRemoteMirrorTestFile(t, filepath.Join(e.shell.Getwd(), ".git", "shallow"))
	wantShallow := readRemoteMirrorTestFile(t, filepath.Join(control, ".git", "shallow"))
	if gotShallow != wantShallow {
		t.Errorf("shallow boundary = %q, want canonical boundary %q", gotShallow, wantShallow)
	}
}

func TestPrepareCheckoutWorkdirRecursiveSubmodulesUseCanonical(t *testing.T) {
	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	commit, _, err := canonical.PushBranch("canonical", "feature-branch")
	if err != nil {
		t.Fatal(err)
	}
	e, _ := newFreshCloneRemoteMirrorExecutor(
		t,
		canonical.RepoURL("canonical"),
		"https://mirror.example/repo.git",
		commit,
	)
	e.GitCloneFlags = "--recurse-submodules"
	attempt := e.resolveRemoteMirrorAttempt(0)

	if err := e.prepareCheckoutWorkdir(
		t.Context(),
		&attempt,
		sparseCheckout{},
		"",
		[]string{"--recurse-submodules"},
		false,
	); err != nil {
		t.Fatalf("prepareCheckoutWorkdir() error = %v", err)
	}
	if attempt.outcome != remoteMirrorOutcomeSkipped || attempt.skipReason != remoteMirrorSkipRecursiveSubmodules {
		t.Errorf("attempt = (%q, %q), want skipped recursive-submodules", attempt.outcome, attempt.skipReason)
	}
	assertGitConfigForRemoteMirrorTest(t, e.shell.Getwd(), "remote.origin.url", e.Repository)
}

func TestPrepareCheckoutWorkdirHidesRemoteURLPromptInDebug(t *testing.T) {
	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	commit, _, err := canonical.PushBranch("canonical", "feature-branch")
	if err != nil {
		t.Fatal(err)
	}
	mirror := copyOnHostMirrorHTTPRepo(t, canonical.RepoURL("canonical"), "mirror")
	e, attempt := newFreshCloneRemoteMirrorExecutor(t, canonical.RepoURL("canonical"), mirror.RepoURL("mirror"), commit)
	logs := &bytes.Buffer{}
	commandOutput := &process.Buffer{}
	debugShell, err := shell.New(
		shell.WithDebug(true),
		shell.WithEnv(e.shell.Env),
		shell.WithLogger(shell.NewWriterLogger(logs, false, nil)),
		shell.WithStdout(commandOutput),
		shell.WithWD(e.shell.Getwd()),
	)
	if err != nil {
		t.Fatal(err)
	}
	e.shell = debugShell

	if err := e.prepareCheckoutWorkdir(t.Context(), &attempt, sparseCheckout{}, "", nil, false); err != nil {
		t.Fatalf("prepareCheckoutWorkdir() error = %v", err)
	}
	if strings.Contains(logs.String(), attempt.url) {
		t.Errorf("debug job log contains remote mirror URL prompt: %q", logs.String())
	}
}

func TestPrepareCheckoutWorkdirRemoteMirrorFailureFallsBack(t *testing.T) {
	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	commit, _, err := canonical.PushBranch("canonical", "feature-branch")
	if err != nil {
		t.Fatal(err)
	}
	e, attempt := newFreshCloneRemoteMirrorExecutor(t, canonical.RepoURL("canonical"), "http://127.0.0.1:1/mirror.git", commit)

	if err := e.prepareCheckoutWorkdir(t.Context(), &attempt, sparseCheckout{}, "", []string{"-v"}, false); err != nil {
		t.Fatalf("prepareCheckoutWorkdir() error = %v", err)
	}
	if attempt.outcome != remoteMirrorOutcomeError {
		t.Errorf("outcome = %q, want error", attempt.outcome)
	}
	assertGitConfigForRemoteMirrorTest(t, e.shell.Getwd(), "remote.origin.url", e.Repository)
	if !hasGitCommit(t.Context(), e.shell, ".git", commit) {
		t.Error("canonical fallback clone does not contain requested commit")
	}
}

func TestPrepareCheckoutWorkdirRemoteMirrorTimeoutFallsBack(t *testing.T) {
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
	e, attempt := newFreshCloneRemoteMirrorExecutor(t, canonical.RepoURL("canonical"), stalled.URL+"/mirror.git", commit)

	if err := e.prepareCheckoutWorkdir(t.Context(), &attempt, sparseCheckout{}, "", []string{"-v"}, false); err != nil {
		t.Fatalf("prepareCheckoutWorkdir() error = %v", err)
	}
	if attempt.outcome != remoteMirrorOutcomeTimeout {
		t.Errorf("outcome = %q, want timeout", attempt.outcome)
	}
	assertGitConfigForRemoteMirrorTest(t, e.shell.Getwd(), "remote.origin.url", e.Repository)
}

func TestPrepareCheckoutWorkdirAllowsDelayedFirstByteBelowStallGuard(t *testing.T) {
	oldLowSpeedTime := remoteMirrorLowSpeedTime
	remoteMirrorLowSpeedTime = 1
	t.Cleanup(func() { remoteMirrorLowSpeedTime = oldLowSpeedTime })

	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	commit, _, err := canonical.PushBranch("canonical", "feature-branch")
	if err != nil {
		t.Fatal(err)
	}
	mirror := copyOnHostMirrorHTTPRepo(t, canonical.RepoURL("canonical"), "mirror")
	target, err := url.Parse(mirror.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	delayed := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		time.Sleep(100 * time.Millisecond)
		proxy.ServeHTTP(rw, req)
	}))
	t.Cleanup(delayed.Close)
	e, attempt := newFreshCloneRemoteMirrorExecutor(t, canonical.RepoURL("canonical"), delayed.URL+"/mirror.git", commit)

	if err := e.prepareCheckoutWorkdir(t.Context(), &attempt, sparseCheckout{}, "", []string{"-v"}, false); err != nil {
		t.Fatalf("prepareCheckoutWorkdir() error = %v", err)
	}
	if attempt.outcome != remoteMirrorOutcomeHit {
		t.Errorf("outcome = %q, want hit below low-speed time", attempt.outcome)
	}
}

func TestPrepareCheckoutWorkdirRemoteMirrorCancellationDoesNotFallback(t *testing.T) {
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
	e, attempt := newFreshCloneRemoteMirrorExecutor(t, canonical.RepoURL("canonical"), hung.URL+"/mirror.git", commit)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	go func() {
		select {
		case <-requested:
			cancel()
		case <-ctx.Done():
		}
	}()
	err = e.prepareCheckoutWorkdir(ctx, &attempt, sparseCheckout{}, "", []string{"-v"}, false)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("prepareCheckoutWorkdir() error = %v, want context.Canceled", err)
	}
	if attempt.outcome != remoteMirrorOutcomeNotReached {
		t.Errorf("outcome = %q, want not reached", attempt.outcome)
	}
	if _, err := os.Stat(filepath.Join(e.shell.Getwd(), ".git")); !os.IsNotExist(err) {
		t.Error("cancelled mirror clone started canonical fallback")
	}
}

func TestPrepareCheckoutWorkdirPartialCloneUsesCanonicalAfterMirrorRemoval(t *testing.T) {
	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	if err := canonical.ConfigureRepository("canonical", "uploadpack.allowFilter", "true"); err != nil {
		t.Fatal(err)
	}
	if err := canonical.ConfigureRepository("canonical", "uploadpack.allowAnySHA1InWant", "true"); err != nil {
		t.Fatal(err)
	}
	commit, _, err := canonical.PushBranch("canonical", "feature-branch")
	if err != nil {
		t.Fatal(err)
	}
	mirror := copyOnHostMirrorHTTPRepo(t, canonical.RepoURL("canonical"), "mirror")
	if err := mirror.ConfigureRepository("mirror", "uploadpack.allowFilter", "true"); err != nil {
		t.Fatal(err)
	}
	e, attempt := newFreshCloneRemoteMirrorExecutor(t, canonical.RepoURL("canonical"), mirror.RepoURL("mirror"), commit)

	if err := e.prepareCheckoutWorkdir(
		t.Context(),
		&attempt,
		sparseCheckout{},
		"",
		[]string{"-v", "--filter=blob:none"},
		true,
	); err != nil {
		t.Fatalf("prepareCheckoutWorkdir() error = %v", err)
	}
	mirror.Close()
	runGitForMirrorTest(t, e.shell.Getwd(), "checkout", "--force", commit)
	if got := readRemoteMirrorTestFile(t, filepath.Join(e.shell.Getwd(), "newfile.txt")); got != "This is a new file." {
		t.Errorf("materialised file = %q, want canonical lazy fetch", got)
	}
}

func TestPrepareCheckoutWorkdirSparseCloneKeepsBloblessShape(t *testing.T) {
	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	commit, _, err := canonical.PushBranch("canonical", "feature-branch")
	if err != nil {
		t.Fatal(err)
	}
	mirror := copyOnHostMirrorHTTPRepo(t, canonical.RepoURL("canonical"), "mirror")
	if err := mirror.ConfigureRepository("mirror", "uploadpack.allowFilter", "true"); err != nil {
		t.Fatal(err)
	}
	e, attempt := newFreshCloneRemoteMirrorExecutor(t, canonical.RepoURL("canonical"), mirror.RepoURL("mirror"), commit)
	sparse := sparseCheckout{paths: []string{"src/"}, mode: SparseCheckoutModeCone}

	if err := e.prepareCheckoutWorkdir(t.Context(), &attempt, sparse, "", []string{"-v"}, false); err != nil {
		t.Fatalf("prepareCheckoutWorkdir() error = %v", err)
	}
	if attempt.outcome != remoteMirrorOutcomeHit {
		t.Fatalf("outcome = %q, want hit", attempt.outcome)
	}

	control := filepath.Join(t.TempDir(), "canonical-sparse")
	runGitForMirrorTest(t, "", "clone", "--sparse", "--filter=blob:none", canonical.RepoURL("canonical"), control)
	for _, key := range []string{
		"core.sparseCheckout",
		"core.sparseCheckoutCone",
		"remote.origin.partialclonefilter",
	} {
		got := gitConfigForRemoteMirrorTest(t, e.shell.Getwd(), key)
		want := gitConfigForRemoteMirrorTest(t, control, key)
		if got != want {
			t.Errorf("%s = %q, want canonical sparse clone value %q", key, got, want)
		}
	}
}

func TestPrepareCheckoutWorkdirUnsupportedFilterSilentlyTransfersAllObjects(t *testing.T) {
	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	commit, _, err := canonical.PushBranch("canonical", "feature-branch")
	if err != nil {
		t.Fatal(err)
	}
	mirror := copyOnHostMirrorHTTPRepo(t, canonical.RepoURL("canonical"), "mirror")
	e, attempt := newFreshCloneRemoteMirrorExecutor(t, canonical.RepoURL("canonical"), mirror.RepoURL("mirror"), commit)

	if err := e.prepareCheckoutWorkdir(
		t.Context(),
		&attempt,
		sparseCheckout{},
		"",
		[]string{"--filter=blob:none"},
		true,
	); err != nil {
		t.Fatalf("prepareCheckoutWorkdir() error = %v", err)
	}
	if attempt.outcome != remoteMirrorOutcomeHit {
		t.Fatalf("outcome = %q, want hit despite unsupported filter", attempt.outcome)
	}
	missing := gitOutputForRemoteCheckoutTest(t, e.shell.Getwd(), "rev-list", "--objects", "--all", "--missing=print")
	for _, line := range strings.Split(missing, "\n") {
		if strings.HasPrefix(line, "?") {
			t.Fatalf("unsupported filter left a missing object %q; want silent full transfer", line)
		}
	}
}

func newFreshCloneRemoteMirrorExecutor(
	t *testing.T,
	canonicalURL, mirrorURL, commit string,
) (*Executor, remoteMirrorAttempt) {
	t.Helper()
	checkout := t.TempDir()
	e := New(ExecutorConfig{
		Repository:         canonicalURL,
		GitRemoteMirrorURL: mirrorURL,
		Commit:             commit,
		Branch:             "feature-branch",
		PullRequest:        "false",
		GitFetchFlags:      "-v --prune",
	})
	e.shell = shell.NewTestShell(t, shell.WithSignalGracePeriod(10*time.Millisecond))
	e.shell.Env.Set("BUILDKITE_BUILD_CHECKOUT_PATH", checkout)
	if err := e.createCheckoutDir(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if e.checkoutRoot != nil {
			_ = e.checkoutRoot.Close()
			e.checkoutRoot = nil
		}
	})
	return e, remoteMirrorAttempt{
		site: remoteMirrorSiteFreshClone,
		url:  mirrorURL,
	}
}

func TestPrepareCheckoutWorkdirPreservesCloneConfig(t *testing.T) {
	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	commit, _, err := canonical.PushBranch("canonical", "feature-branch")
	if err != nil {
		t.Fatal(err)
	}
	mirror := copyOnHostMirrorHTTPRepo(t, canonical.RepoURL("canonical"), "mirror")
	if err := mirror.ConfigureRepository("mirror", "uploadpack.allowFilter", "true"); err != nil {
		t.Fatal(err)
	}
	referenceMirror := filepath.Join(t.TempDir(), "reference.git")
	cloneOnHostMirrorToPath(t, canonical.RepoURL("canonical"), referenceMirror)

	tests := []struct {
		name         string
		flags        []string
		referenceDir string
		mirrorMode   string
	}{
		{name: "depth", flags: []string{"--depth=1"}},
		{name: "filter", flags: []string{"--filter=blob:none"}},
		{name: "single branch", flags: []string{"--single-branch"}},
		{name: "no tags", flags: []string{"--no-tags"}},
		{name: "reference", referenceDir: referenceMirror, mirrorMode: "reference"},
		{name: "dissociate", referenceDir: referenceMirror, mirrorMode: "dissociate"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e, attempt := newFreshCloneRemoteMirrorExecutor(t, canonical.RepoURL("canonical"), mirror.RepoURL("mirror"), commit)
			e.GitMirrorCheckoutMode = tc.mirrorMode
			if err := e.prepareCheckoutWorkdir(t.Context(), &attempt, sparseCheckout{}, tc.referenceDir, tc.flags, hasPartialFilterFlags(tc.flags)); err != nil {
				t.Fatalf("prepareCheckoutWorkdir() error = %v", err)
			}
			if attempt.outcome != remoteMirrorOutcomeHit && attempt.outcome != remoteMirrorOutcomeMiss {
				t.Fatalf("outcome = %q, want successful mirror clone (hit or lag miss)", attempt.outcome)
			}
			if got := gitConfigForRemoteMirrorTest(t, e.shell.Getwd(), "remote.origin.url"); got != canonical.RepoURL("canonical") {
				t.Errorf("origin = %q, want canonical", got)
			}

			control := filepath.Join(t.TempDir(), "canonical-control")
			cloneArgs := append([]string{"clone"}, tc.flags...)
			if tc.referenceDir != "" {
				cloneArgs = append(cloneArgs, "--reference", tc.referenceDir)
				if tc.mirrorMode == "dissociate" {
					cloneArgs = append(cloneArgs, "--dissociate")
				}
			}
			cloneArgs = append(cloneArgs, canonical.RepoURL("canonical"), control)
			runGitForMirrorTest(t, "", cloneArgs...)
			for _, key := range []string{
				"remote.origin.fetch",
				"remote.origin.tagOpt",
				"remote.origin.promisor",
				"remote.origin.partialclonefilter",
			} {
				got := gitConfigForRemoteMirrorTest(t, e.shell.Getwd(), key)
				want := gitConfigForRemoteMirrorTest(t, control, key)
				if got != want {
					t.Errorf("%s = %q, want canonical clone value %q", key, got, want)
				}
			}
			gotShallow := gitOutputForRemoteCheckoutTest(t, e.shell.Getwd(), "rev-parse", "--is-shallow-repository")
			wantShallow := gitOutputForRemoteCheckoutTest(t, control, "rev-parse", "--is-shallow-repository")
			if gotShallow != wantShallow {
				t.Errorf("shallow repository = %q, want canonical clone value %q", gotShallow, wantShallow)
			}
			gotAlternates := osutil.FileExists(filepath.Join(e.shell.Getwd(), ".git", "objects", "info", "alternates"))
			wantAlternates := osutil.FileExists(filepath.Join(control, ".git", "objects", "info", "alternates"))
			if gotAlternates != wantAlternates {
				t.Errorf("alternates present = %t, want canonical clone value %t", gotAlternates, wantAlternates)
			}
		})
	}
}

func TestPrepareCheckoutWorkdirRequiredSmudgeFailureFallsBackToCanonical(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test smudge shim is POSIX-only")
	}

	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	source := filepath.Join(t.TempDir(), "source")
	runGitForMirrorTest(t, "", "clone", canonical.RepoURL("canonical"), source)
	runGitForMirrorTest(t, source, "checkout", "-B", "main", "origin/main")
	if err := os.WriteFile(filepath.Join(source, ".gitattributes"), []byte("*.bin filter=testfilter\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "asset.bin"), []byte("pointer-content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitForMirrorTest(t, source, "add", ".gitattributes", "asset.bin")
	runGitForMirrorTest(t, source, "commit", "-m", "Add filtered asset")
	runGitForMirrorTest(t, source, "push", "origin", "HEAD:refs/heads/main")
	commit := gitOutputForRemoteCheckoutTest(t, source, "rev-parse", "HEAD")
	mirror := copyOnHostMirrorHTTPRepo(t, canonical.RepoURL("canonical"), "mirror")

	state := filepath.Join(t.TempDir(), "smudge-attempted")
	script := filepath.Join(t.TempDir(), "smudge")
	body := "#!/bin/sh\nif [ ! -e " + shellwords.Quote(state) + " ]; then : > " + shellwords.Quote(state) + "; exit 1; fi\ncat\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	e, attempt := newFreshCloneRemoteMirrorExecutor(t, canonical.RepoURL("canonical"), mirror.RepoURL("mirror"), commit)
	flags := []string{
		"--branch=main",
		"--config", "filter.testfilter.smudge=" + script,
		"--config", "filter.testfilter.required=true",
	}

	if err := e.prepareCheckoutWorkdir(t.Context(), &attempt, sparseCheckout{}, "", flags, false); err != nil {
		t.Fatalf("prepareCheckoutWorkdir() error = %v", err)
	}
	if attempt.outcome != remoteMirrorOutcomeError {
		t.Errorf("outcome = %q, want mirror clone error", attempt.outcome)
	}
	if got := readRemoteMirrorTestFile(t, filepath.Join(e.shell.Getwd(), "asset.bin")); got != "pointer-content" {
		t.Errorf("asset content = %q, want canonical fallback to materialise it", got)
	}
}

func TestPrepareCheckoutWorkdirGitLFSBehavior(t *testing.T) {
	if err := exec.Command("git", "lfs", "version").Run(); err != nil {
		t.Skip("git-lfs is not installed")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_LFS_SKIP_SMUDGE", "0")
	runGitForMirrorTest(t, "", "lfs", "install", "--skip-repo")
	t.Setenv("GIT_AUTHOR_NAME", "Buildkite Agent")
	t.Setenv("GIT_AUTHOR_EMAIL", "agent@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Buildkite Agent")
	t.Setenv("GIT_COMMITTER_EMAIL", "agent@example.com")

	canonical := filepath.Join(t.TempDir(), "canonical.git")
	runGitForMirrorTest(t, "", "init", "--bare", canonical)
	source := filepath.Join(t.TempDir(), "source")
	runGitForMirrorTest(t, "", "init", "--initial-branch=main", source)
	runGitForMirrorTest(t, source, "lfs", "install", "--local")
	runGitForMirrorTest(t, source, "lfs", "track", "*.bin")
	asset := bytes.Repeat([]byte("lfs-content-"), 10_000)
	if err := os.WriteFile(filepath.Join(source, "asset.bin"), asset, 0o644); err != nil {
		t.Fatal(err)
	}
	runGitForMirrorTest(t, source, "add", ".gitattributes", "asset.bin")
	runGitForMirrorTest(t, source, "commit", "-m", "Add LFS asset")
	runGitForMirrorTest(t, source, "remote", "add", "origin", canonical)
	runGitForMirrorTest(t, source, "push", "origin", "main")
	runGitForMirrorTest(t, source, "lfs", "push", "origin", "--all")
	runGitForMirrorTest(t, "", "--git-dir", canonical, "symbolic-ref", "HEAD", "refs/heads/main")
	commit := gitOutputForRemoteCheckoutTest(t, source, "rev-parse", "HEAD")

	mirror := copyOnHostMirrorHTTPRepo(t, canonical, "mirror")
	if err := mirror.SetDefaultBranch("mirror", "main"); err != nil {
		t.Fatal(err)
	}
	control := filepath.Join(t.TempDir(), "canonical-control")
	runGitForMirrorTest(t, "", "clone", canonical, control)
	controlInfo, err := os.Stat(filepath.Join(control, "asset.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := controlInfo.Size(), int64(len(asset)); got != want {
		t.Fatalf("canonical control asset size = %d, want %d", got, want)
	}

	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("enabled=%t", enabled), func(t *testing.T) {
			e, attempt := newFreshCloneRemoteMirrorExecutor(t, canonical, mirror.RepoURL("mirror"), commit)
			e.GitLFSEnabled = enabled
			e.shell.Env.Set("HOME", home)
			e.shell.Env.Set("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig"))
			e.shell.Env.Set("GIT_CONFIG_COUNT", "3")
			e.shell.Env.Set("GIT_CONFIG_KEY_0", "filter.lfs.smudge")
			e.shell.Env.Set("GIT_CONFIG_VALUE_0", "git-lfs smudge -- %f")
			e.shell.Env.Set("GIT_CONFIG_KEY_1", "filter.lfs.process")
			e.shell.Env.Set("GIT_CONFIG_VALUE_1", "git-lfs filter-process")
			e.shell.Env.Set("GIT_CONFIG_KEY_2", "filter.lfs.required")
			e.shell.Env.Set("GIT_CONFIG_VALUE_2", "true")
			if enabled {
				// This is existing executor behavior, not a mirror-specific
				// workaround. The later LFS phase materialises from canonical.
				e.shell.Env.Set("GIT_LFS_SKIP_SMUDGE", "1")
			}

			if err := e.prepareCheckoutWorkdir(t.Context(), &attempt, sparseCheckout{}, "", nil, false); err != nil {
				t.Fatalf("prepareCheckoutWorkdir() error = %v", err)
			}
			if enabled {
				if attempt.outcome != remoteMirrorOutcomeHit {
					t.Errorf("outcome = %q, want hit with existing skip-smudge behavior", attempt.outcome)
				}
				if err := gitLFSFetchCheckout(t.Context(), gitLFSFetchCheckoutArgs{Shell: e.shell}); err != nil {
					t.Fatalf("gitLFSFetchCheckout() error = %v", err)
				}
			} else if attempt.outcome != remoteMirrorOutcomeError {
				t.Errorf("outcome = %q, want loud mirror LFS error and canonical fallback", attempt.outcome)
			}

			info, err := os.Stat(filepath.Join(e.shell.Getwd(), "asset.bin"))
			if err != nil {
				t.Fatal(err)
			}
			wantSize := int64(len(asset))
			if got := info.Size(); got != wantSize {
				t.Errorf("asset size = %d, want %d (enabled=%t)", got, wantSize, enabled)
			}
		})
	}
}
