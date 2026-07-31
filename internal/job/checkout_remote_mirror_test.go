package job

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/buildkite/agent/v3/internal/job/githttptest"
	"github.com/buildkite/agent/v3/internal/self"
	"github.com/buildkite/agent/v3/internal/shell"
	"github.com/buildkite/shellwords"
)

func TestIsFullCommitSHA(t *testing.T) {
	t.Parallel()

	valid := strings.Repeat("0123456789abcdef", 2) + "01234567"
	tests := map[string]bool{
		valid:                   true,
		strings.ToUpper(valid):  false,
		valid[:39]:              false,
		valid + "0":             false,
		"HEAD":                  false,
		"main":                  false,
		strings.Repeat("g", 40): false,
	}
	for commit, want := range tests {
		if got := isFullCommitSHA(commit); got != want {
			t.Errorf("isFullCommitSHA(%q) = %t, want %t", commit, got, want)
		}
	}
}

func TestShouldAttemptRemoteMirror(t *testing.T) {
	t.Parallel()

	commit := strings.Repeat("a", 40)
	base := ExecutorConfig{
		Commit:             commit,
		GitRemoteMirrorURL: "https://mirror.example.com/acme/widgets.git",
		PullRequest:        "false",
	}
	tests := []struct {
		name             string
		mutate           func(*ExecutorConfig)
		previousAttempts int
		want             bool
	}{
		{name: "eligible", want: true},
		{name: "empty URL", mutate: func(c *ExecutorConfig) { c.GitRemoteMirrorURL = "" }},
		{name: "HEAD", mutate: func(c *ExecutorConfig) { c.Commit = "HEAD" }},
		{name: "short SHA", mutate: func(c *ExecutorConfig) { c.Commit = commit[:12] }},
		{name: "uppercase SHA", mutate: func(c *ExecutorConfig) { c.Commit = strings.ToUpper(commit) }},
		{name: "tag", mutate: func(c *ExecutorConfig) { c.Tag = "v1.0.0" }},
		{name: "pull request", mutate: func(c *ExecutorConfig) { c.PullRequest = "42" }},
		{name: "custom refspec", mutate: func(c *ExecutorConfig) { c.RefSpec = "refs/heads/main" }},
		{name: "later checkout attempt", previousAttempts: 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			if tc.mutate != nil {
				tc.mutate(&cfg)
			}
			e := &Executor{ExecutorConfig: cfg}
			if got := e.shouldAttemptRemoteMirror(tc.previousAttempts); got != tc.want {
				t.Errorf("shouldAttemptRemoteMirror(%d) = %t, want %t", tc.previousAttempts, got, tc.want)
			}
		})
	}
}

func TestRemoteMirrorTimeoutReservesCanonicalFallbackBudget(t *testing.T) {
	t.Parallel()

	if got := remoteMirrorTimeout(t.Context()); got != remoteMirrorAttemptTimeout {
		t.Errorf("remoteMirrorTimeout(without deadline) = %s, want %s", got, remoteMirrorAttemptTimeout)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 9*time.Second)
	defer cancel()
	got := remoteMirrorTimeout(ctx)
	if got <= 2*time.Second || got > 3*time.Second {
		t.Errorf("remoteMirrorTimeout(9s deadline) = %s, want approximately 3s", got)
	}
}

func TestRemoteMirrorCredentialHelperFlagsAreInvocationScoped(t *testing.T) {
	t.Parallel()

	parts, err := shellwords.Split(gitCredentialHelperFlags(t.Context()))
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 6 {
		t.Fatalf("credential helper flags split into %d parts, want 6: %q", len(parts), parts)
	}
	if parts[0] != "-c" || parts[1] != "credential.useHttpPath=true" {
		t.Errorf("credential helper flags do not enable HTTP path matching: %q", parts)
	}
	if parts[2] != "-c" || parts[3] != "credential.helper=" {
		t.Errorf("credential helper flags do not clear inherited helpers: %q", parts)
	}
	if parts[4] != "-c" || !strings.HasPrefix(parts[5], "credential.helper=") ||
		!strings.HasSuffix(parts[5], " git-credentials-helper") {
		t.Errorf("credential helper flags do not install the agent helper: %q", parts)
	}
}

func TestRemoteMirrorHitChecksOutExactCommitWithCanonicalOrigin(t *testing.T) {
	e := newRemoteMirrorTestExecutor(t)
	canonical, commit := setupCheckoutTestRepo(t, e, "canonical")
	mirror := copyRemoteMirrorRepository(t, canonical.RepoURL("canonical"), "mirror")

	e.Commit = commit
	e.Branch = "feature-branch"
	e.PullRequest = "false"
	e.GitRemoteMirrorURL = mirror.RepoURL("mirror")
	canonicalURL := e.Repository

	// A mirror hit must not need canonical transport.
	canonical.Close()

	if err := e.defaultCheckoutPhase(t.Context(), 0); err != nil {
		t.Fatalf("defaultCheckoutPhase() error = %v", err)
	}

	assertCheckedOutCommit(t, e, commit)
	assertOriginURL(t, e, canonicalURL)
	assertNoMutableRefs(t, e)
}

func TestRemoteMirrorHitPreservesRepeatedCloneConfigs(t *testing.T) {
	e := newRemoteMirrorTestExecutor(t)
	canonical, commit := setupCheckoutTestRepo(t, e, "canonical")
	mirror := copyRemoteMirrorRepository(t, canonical.RepoURL("canonical"), "mirror")

	e.Commit = commit
	e.Branch = "feature-branch"
	e.PullRequest = "false"
	e.GitRemoteMirrorURL = mirror.RepoURL("mirror")
	// git clone --config is additive for repeated keys, so a fresh mirror hit
	// must persist every value rather than only the last one.
	e.GitCloneFlags = "-v --config http.extraHeader=a --config http.extraHeader=b"
	canonical.Close()

	if err := e.defaultCheckoutPhase(t.Context(), 0); err != nil {
		t.Fatalf("defaultCheckoutPhase() error = %v", err)
	}

	assertCheckedOutCommit(t, e, commit)
	got, err := e.shell.Command("git", "config", "--local", "--get-all", "http.extraHeader").RunAndCaptureStdout(t.Context())
	if err != nil {
		t.Fatalf("reading http.extraHeader: %v", err)
	}
	if want := "a\nb"; got != want {
		t.Errorf("http.extraHeader = %q, want %q", got, want)
	}
}

func TestAuthenticatedRemoteMirrorUsesRepositoryAccessToken(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("credential helper subprocess shim is POSIX-only")
	}

	e := newRemoteMirrorTestExecutor(t)
	canonical, commit := setupCheckoutTestRepo(t, e, "canonical")
	mirror := copyRemoteMirrorRepository(t, canonical.RepoURL("canonical"), "mirror")

	mirrorURL, err := url.Parse(mirror.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(mirrorURL)
	var tokenRequests atomic.Int32
	var authenticatedGitRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/jobs/job-id/repository_access_token" {
			tokenRequests.Add(1)
			var body struct {
				RepoURL string `json:"repo_url"`
			}
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if got, want := body.RepoURL, serverURL(req)+"/mirror.git"; got != want {
				t.Errorf("repo_url = %q, want %q", got, want)
			}
			_, _ = io.WriteString(rw, `{"token":"mirror-password"}`)
			return
		}

		username, password, ok := req.BasicAuth()
		if !ok || username != "token" || password != "mirror-password" {
			rw.Header().Set("WWW-Authenticate", `Basic realm="repository"`)
			http.Error(rw, "authentication required", http.StatusUnauthorized)
			return
		}
		authenticatedGitRequests.Add(1)
		proxy.ServeHTTP(rw, req)
	}))
	t.Cleanup(server.Close)

	helperPath := filepath.Join(t.TempDir(), "buildkite-agent-helper")
	testBinary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	helperScript := fmt.Sprintf("#!/bin/sh\nexec %q -test.run=TestRepositoryCredentialHelperProcess -- \"$@\"\n", testBinary)
	if err := os.WriteFile(helperPath, []byte(helperScript), 0o755); err != nil {
		t.Fatal(err)
	}

	e.Commit = commit
	e.Branch = "feature-branch"
	e.PullRequest = "false"
	e.GitRemoteMirrorURL = server.URL + "/mirror.git"
	e.shell.Env.Set("BUILDKITE_AGENT_ENDPOINT", server.URL)
	e.shell.Env.Set("BUILDKITE_JOB_ID", "job-id")
	e.shell.Env.Set("GO_WANT_REPOSITORY_CREDENTIAL_HELPER", "1")
	canonical.Close()

	ctx := self.OverridePath(t.Context(), helperPath)
	if err := e.defaultCheckoutPhase(ctx, 0); err != nil {
		t.Fatalf("defaultCheckoutPhase() error = %v", err)
	}
	assertCheckedOutCommit(t, e, commit)
	if tokenRequests.Load() == 0 {
		t.Error("repository_access_token endpoint was not requested")
	}
	if authenticatedGitRequests.Load() == 0 {
		t.Error("remote mirror did not receive authenticated Git requests")
	}
}

func TestRepositoryCredentialHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_REPOSITORY_CREDENTIAL_HELPER") != "1" {
		return
	}

	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		os.Exit(1)
	}
	components := make(map[string]string)
	for line := range strings.SplitSeq(string(input), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok {
			components[key] = value
		}
	}
	repoURL := (&url.URL{
		Scheme: components["protocol"],
		Host:   components["host"],
		Path:   components["path"],
	}).String()
	body, _ := json.Marshal(map[string]string{"repo_url": repoURL})
	endpoint := os.Getenv("BUILDKITE_AGENT_ENDPOINT") + "/jobs/" + os.Getenv("BUILDKITE_JOB_ID") + "/repository_access_token"
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, endpoint, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		os.Exit(1)
	}
	var tokenResponse struct {
		Token string `json:"token"`
	}
	err = json.NewDecoder(resp.Body).Decode(&tokenResponse)
	_ = resp.Body.Close()
	if err != nil {
		os.Exit(1)
	}
	fmt.Printf("username=token\npassword=%s\n\n", tokenResponse.Token)
	os.Exit(0)
}

func serverURL(req *http.Request) string {
	return "http://" + req.Host
}

func TestRemoteMirrorMissFallsBackToCanonical(t *testing.T) {
	e := newRemoteMirrorTestExecutor(t)
	_, commit := setupCheckoutTestRepo(t, e, "canonical")

	mirror := githttptest.NewServer()
	t.Cleanup(mirror.Close)
	if err := mirror.CreateRepository("mirror"); err != nil {
		t.Fatal(err)
	}
	if _, err := mirror.InitRepository("mirror"); err != nil {
		t.Fatal(err)
	}

	e.Commit = commit
	e.Branch = "feature-branch"
	e.PullRequest = "false"
	e.GitRemoteMirrorURL = mirror.RepoURL("mirror")

	if err := e.defaultCheckoutPhase(t.Context(), 0); err != nil {
		t.Fatalf("defaultCheckoutPhase() error = %v", err)
	}

	assertCheckedOutCommit(t, e, commit)
	assertOriginURL(t, e, e.Repository)
}

func TestRemoteMirrorFailureFallsBackToCanonical(t *testing.T) {
	e := newRemoteMirrorTestExecutor(t)
	_, commit := setupCheckoutTestRepo(t, e, "canonical")

	e.Commit = commit
	e.Branch = "feature-branch"
	e.PullRequest = "false"
	e.GitRemoteMirrorURL = "http://127.0.0.1:1/acme/widgets.git"

	if err := e.defaultCheckoutPhase(t.Context(), 0); err != nil {
		t.Fatalf("defaultCheckoutPhase() error = %v", err)
	}

	assertCheckedOutCommit(t, e, commit)
	assertOriginURL(t, e, e.Repository)
}

func TestRemoteMirrorTimeoutFallsBackWithinCheckoutDeadline(t *testing.T) {
	e := newRemoteMirrorTestExecutor(t)
	_, commit := setupCheckoutTestRepo(t, e, "canonical")
	hungMirror := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(hungMirror.Close)

	e.Commit = commit
	e.Branch = "feature-branch"
	e.PullRequest = "false"
	e.GitRemoteMirrorURL = hungMirror.URL + "/acme/widgets.git"

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err := e.defaultCheckoutPhase(ctx, 0); err != nil {
		t.Fatalf("defaultCheckoutPhase() error = %v", err)
	}
	assertCheckedOutCommit(t, e, commit)
}

func TestRemoteMirrorAndCanonicalFailureReturnsCanonicalError(t *testing.T) {
	e := newRemoteMirrorTestExecutor(t)
	canonical, commit := setupCheckoutTestRepo(t, e, "canonical")
	e.Commit = commit
	e.Branch = "feature-branch"
	e.PullRequest = "false"
	e.GitRemoteMirrorURL = "http://127.0.0.1:1/acme/widgets.git"
	canonical.Close()

	err := e.defaultCheckoutPhase(t.Context(), 0)
	if err == nil {
		t.Fatal("defaultCheckoutPhase() error = nil, want canonical clone error")
	}
	if !strings.Contains(err.Error(), "cloning git repository") {
		t.Errorf("defaultCheckoutPhase() error = %q, want canonical clone context", err)
	}
	if strings.Contains(err.Error(), e.GitRemoteMirrorURL) {
		t.Errorf("terminal canonical error contains remote mirror URL: %q", err)
	}
}

func TestRemoteMirrorFetchesIntoReusedCanonicalCheckout(t *testing.T) {
	e := newRemoteMirrorTestExecutor(t)
	canonical, initialCommit := setupCheckoutTestRepo(t, e, "canonical")
	e.Commit = initialCommit
	e.Branch = "feature-branch"
	e.PullRequest = "false"

	if err := e.defaultCheckoutPhase(t.Context(), 0); err != nil {
		t.Fatalf("initial defaultCheckoutPhase() error = %v", err)
	}

	mirror := copyRemoteMirrorRepository(t, canonical.RepoURL("canonical"), "mirror")
	mirrorOnlyCommit := pushUniqueCommit(t, mirror, "mirror", "mirror-only", "mirror-only.txt", "mirror only")
	if mirrorOnlyCommit == initialCommit {
		t.Fatal("pushUniqueCommit did not create a new commit")
	}

	e.Commit = mirrorOnlyCommit
	e.Branch = "mirror-only"
	e.GitRemoteMirrorURL = mirror.RepoURL("mirror")
	canonicalURL := e.Repository
	canonical.Close()

	if err := e.defaultCheckoutPhase(t.Context(), 0); err != nil {
		t.Fatalf("reused defaultCheckoutPhase() error = %v", err)
	}

	assertCheckedOutCommit(t, e, mirrorOnlyCommit)
	assertOriginURL(t, e, canonicalURL)
}

func TestRemoteMirrorMissPreservesReusedCanonicalCheckout(t *testing.T) {
	e := newRemoteMirrorTestExecutor(t)
	canonical, initialCommit := setupCheckoutTestRepo(t, e, "canonical")
	e.Commit = initialCommit
	e.Branch = "feature-branch"
	e.PullRequest = "false"

	if err := e.defaultCheckoutPhase(t.Context(), 0); err != nil {
		t.Fatalf("initial defaultCheckoutPhase() error = %v", err)
	}
	if err := e.shell.Command("git", "branch", "preserve-me", initialCommit).Run(t.Context()); err != nil {
		t.Fatal(err)
	}

	// Snapshot the initial checkout into the mirror, then advance only the
	// canonical repository so the mirror miss path must fall back.
	mirror := copyRemoteMirrorRepository(t, canonical.RepoURL("canonical"), "mirror")
	nextCommit := pushUniqueCommit(t, canonical, "canonical", "feature-branch", "canonical-only.txt", "canonical only")
	if nextCommit == initialCommit {
		t.Fatal("pushUniqueCommit did not create a new commit")
	}

	e.Commit = nextCommit
	e.GitRemoteMirrorURL = mirror.RepoURL("mirror")
	if err := e.defaultCheckoutPhase(t.Context(), 0); err != nil {
		t.Fatalf("defaultCheckoutPhase() error = %v", err)
	}

	assertCheckedOutCommit(t, e, nextCommit)
	if got, err := e.shell.Command("git", "rev-parse", "preserve-me").RunAndCaptureStdout(t.Context()); err != nil {
		t.Fatalf("reused checkout was discarded after mirror miss: %v", err)
	} else if got != initialCommit {
		t.Errorf("preserve-me = %q, want %q", got, initialCommit)
	}
}

func TestRemoteMirrorFetchFlagsStripPartialCloneFilters(t *testing.T) {
	t.Parallel()

	got, err := remoteMirrorFetchFlags(`-v --prune --filter=blob:none --filter tree:0 --filt=blob:none`)
	if err != nil {
		t.Fatal(err)
	}
	if want := "-v --prune"; got != want {
		t.Errorf("remoteMirrorFetchFlags() = %q, want %q", got, want)
	}
	got, err = remoteMirrorFetchFlags("-v")
	if err != nil {
		t.Fatal(err)
	}
	if got != "-v" {
		t.Errorf("remoteMirrorFetchFlags() = %q, want %q", got, "-v")
	}
}

func TestIsPartialCloneCheckoutDetectsPromisorRemotes(t *testing.T) {
	e := newRemoteMirrorTestExecutor(t)
	dir := t.TempDir()
	if err := e.shell.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	if err := e.shell.Command("git", "init").Run(t.Context()); err != nil {
		t.Fatal(err)
	}
	if e.isPartialCloneCheckout(t.Context()) {
		t.Fatal("fresh repo reported as partial clone")
	}
	if err := e.shell.Command("git", "config", "remote.origin.promisor", "true").Run(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := e.shell.Command("git", "config", "remote.origin.partialclonefilter", "blob:none").Run(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !e.isPartialCloneCheckout(t.Context()) {
		t.Fatal("promisor remote not detected as partial clone")
	}
}

func TestRemoteMirrorCompatibleCloneConfigs(t *testing.T) {
	t.Parallel()

	configs, ok := remoteMirrorCompatibleCloneConfigs([]string{"-v", "--config", "core.autocrlf=false"})
	if !ok {
		t.Fatal("expected clone flags to be compatible")
	}
	if len(configs) != 1 || configs[0] != [2]string{"core.autocrlf", "false"} {
		t.Fatalf("configs = %#v, want core.autocrlf=false", configs)
	}
	if _, ok := remoteMirrorCompatibleCloneConfigs([]string{"-v", "--filter=blob:none"}); ok {
		t.Fatal("expected filter clone flags to be incompatible")
	}
	if _, ok := remoteMirrorCompatibleCloneConfigs([]string{"--depth", "1"}); ok {
		t.Fatal("expected depth clone flags to be incompatible")
	}
}

func TestRemoteMirrorHitDoesNotTouchOnHostMirror(t *testing.T) {
	e := newRemoteMirrorTestExecutor(t)
	canonical, commit := setupCheckoutTestRepo(t, e, "canonical")
	mirror := copyRemoteMirrorRepository(t, canonical.RepoURL("canonical"), "mirror")

	e.Commit = commit
	e.Branch = "feature-branch"
	e.PullRequest = "false"
	e.GitRemoteMirrorURL = mirror.RepoURL("mirror")
	e.GitMirrorsPath = filepath.Join(t.TempDir(), "on-host-mirrors")
	canonical.Close()

	if err := e.defaultCheckoutPhase(t.Context(), 0); err != nil {
		t.Fatalf("defaultCheckoutPhase() error = %v", err)
	}
	if _, err := os.Stat(e.GitMirrorsPath); !os.IsNotExist(err) {
		t.Errorf("on-host mirror path was touched, os.Stat error = %v", err)
	}
	if _, exists := e.shell.Env.Get("BUILDKITE_REPO_MIRROR"); exists {
		t.Error("BUILDKITE_REPO_MIRROR was set on a remote mirror hit")
	}
}

func TestIneligibleRemoteMirrorTargetsUseCanonical(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Executor)
	}{
		{name: "HEAD", mutate: func(e *Executor) { e.Commit = "HEAD" }},
		{name: "short SHA", mutate: func(e *Executor) { e.Commit = e.Commit[:12] }},
		{name: "uppercase SHA", mutate: func(e *Executor) { e.Commit = strings.ToUpper(e.Commit) }},
		{name: "tag", mutate: func(e *Executor) { e.Tag = "v1.0.0" }},
		{name: "pull request", mutate: func(e *Executor) { e.PullRequest = "42" }},
		{name: "custom refspec", mutate: func(e *Executor) { e.RefSpec = "refs/heads/feature-branch" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newRemoteMirrorTestExecutor(t)
			_, commit := setupCheckoutTestRepo(t, e, "canonical")
			e.Commit = commit
			e.Branch = "feature-branch"
			e.PullRequest = "false"
			e.GitRemoteMirrorURL = "http://127.0.0.1:1/acme/widgets.git"
			tc.mutate(e)

			if err := e.defaultCheckoutPhase(t.Context(), 0); err != nil {
				t.Fatalf("defaultCheckoutPhase() error = %v", err)
			}
		})
	}
}

func newRemoteMirrorTestExecutor(t *testing.T) *Executor {
	t.Helper()
	sh, err := shell.New()
	if err != nil {
		t.Fatal(err)
	}
	return &Executor{
		shell: sh,
		ExecutorConfig: ExecutorConfig{
			GitCheckoutFlags: "-f",
			GitCleanFlags:    "-ffxdq",
			GitCloneFlags:    "-v",
			GitFetchFlags:    "-v --prune",
		},
	}
}

func copyRemoteMirrorRepository(t *testing.T, sourceURL, name string) *githttptest.Server {
	t.Helper()
	mirror := githttptest.NewServer()
	t.Cleanup(mirror.Close)
	if err := mirror.CreateRepository(name); err != nil {
		t.Fatal(err)
	}

	cloneDir := t.TempDir()
	runGitForRemoteMirrorTest(t, "", "clone", "--mirror", sourceURL, cloneDir)
	// Bare mirrors copied this way often lack a resolvable HEAD; point it at a
	// real branch so later clones/checkouts can create distinct commits.
	runGitForRemoteMirrorTest(t, cloneDir, "symbolic-ref", "HEAD", "refs/heads/feature-branch")
	runGitForRemoteMirrorTest(t, cloneDir, "push", "--mirror", mirror.RepoURL(name))
	return mirror
}

// pushUniqueCommit advances repoName on branchName with unique content so the
// resulting SHA differs from prior PushBranch commits that reuse fixed files.
func pushUniqueCommit(t *testing.T, server *githttptest.Server, repoName, branchName, fileName, contents string) string {
	t.Helper()
	tmp := t.TempDir()
	runGitForRemoteMirrorTest(t, "", "clone", server.RepoURL(repoName), tmp)
	if err := exec.Command("git", "-C", tmp, "rev-parse", "--verify", "origin/"+branchName).Run(); err == nil {
		runGitForRemoteMirrorTest(t, tmp, "checkout", "-B", branchName, "origin/"+branchName)
	} else {
		runGitForRemoteMirrorTest(t, tmp, "checkout", "-B", branchName)
	}
	if err := os.WriteFile(filepath.Join(tmp, fileName), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitForRemoteMirrorTest(t, tmp, "add", fileName)
	runGitForRemoteMirrorTest(t, tmp, "commit", "-m", "Add "+fileName)
	runGitForRemoteMirrorTest(t, tmp, "push", "origin", branchName)
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = tmp
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse HEAD error = %v", err)
	}
	return strings.TrimSpace(string(out))
}

func runGitForRemoteMirrorTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Buildkite Agent",
		"GIT_AUTHOR_EMAIL=agent@example.com",
		"GIT_COMMITTER_NAME=Buildkite Agent",
		"GIT_COMMITTER_EMAIL=agent@example.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v error = %v\n%s", args, err, out)
	}
}

func assertCheckedOutCommit(t *testing.T, e *Executor, want string) {
	t.Helper()
	got, err := e.shell.Command("git", "rev-parse", "HEAD").RunAndCaptureStdout(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("checked out commit = %q, want %q", got, want)
	}
}

func assertOriginURL(t *testing.T, e *Executor, want string) {
	t.Helper()
	got, err := e.shell.Command("git", "remote", "get-url", "origin").RunAndCaptureStdout(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("origin URL = %q, want %q", got, want)
	}
}

func assertNoMutableRefs(t *testing.T, e *Executor) {
	t.Helper()
	got, err := e.shell.Command(
		"git", "for-each-ref", "--format=%(refname)",
		"refs/heads", "refs/remotes", "refs/tags",
	).RunAndCaptureStdout(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(got) != "" {
		t.Errorf("mirror-derived mutable refs remain: %s", got)
	}

	configPath := filepath.Join(e.shell.Getwd(), ".git", "config")
	config, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(config), e.GitRemoteMirrorURL) {
		t.Errorf(".git/config retains remote mirror URL")
	}
	if strings.Contains(string(config), "credential.helper") {
		t.Errorf(".git/config persists credential helper")
	}
}
