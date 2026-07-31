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
	"slices"
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
		{name: "local path", mutate: func(c *ExecutorConfig) { c.GitRemoteMirrorURL = "file:///tmp/mirror.git" }},
		{name: "SSH URL", mutate: func(c *ExecutorConfig) { c.GitRemoteMirrorURL = "ssh://mirror.example.com/repo.git" }},
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

func TestRemoteMirrorHitPreservesShallowDepth(t *testing.T) {
	e := newRemoteMirrorTestExecutor(t)
	canonical, commit := setupCheckoutTestRepo(t, e, "canonical")
	for i := range 4 {
		commit = pushUniqueCommit(t, canonical, "canonical", "feature-branch", fmt.Sprintf("depth-%d.txt", i), "depth")
	}
	mirror := copyRemoteMirrorRepository(t, canonical.RepoURL("canonical"), "mirror")

	e.Commit = commit
	e.Branch = "feature-branch"
	e.PullRequest = "false"
	e.GitRemoteMirrorURL = mirror.RepoURL("mirror")
	e.GitCloneFlags = "-v --depth=2"
	e.GitFetchFlags = "-v --prune --depth=2"
	canonical.Close()

	if err := e.defaultCheckoutPhase(t.Context(), 0); err != nil {
		t.Fatalf("defaultCheckoutPhase() error = %v", err)
	}

	assertCheckedOutCommit(t, e, commit)
	got, err := e.shell.Command("git", "rev-list", "--count", "HEAD").RunAndCaptureStdout(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got != "2" {
		t.Errorf("shallow commit count = %q, want 2", got)
	}
	if _, err := os.Stat(filepath.Join(e.shell.Getwd(), ".git", "shallow")); err != nil {
		t.Errorf("shallow boundary file: %v", err)
	}
}

func TestRemoteMirrorHitRetargetsPartialCloneToCanonicalOrigin(t *testing.T) {
	e := newRemoteMirrorTestExecutor(t)
	canonical, commit := setupCheckoutTestRepo(t, e, "canonical")
	mirror := copyRemoteMirrorRepository(t, canonical.RepoURL("canonical"), "mirror")
	if err := canonical.ConfigureRepository("canonical", "uploadpack.allowFilter", "true"); err != nil {
		t.Fatal(err)
	}
	if err := canonical.ConfigureRepository("canonical", "uploadpack.allowAnySHA1InWant", "true"); err != nil {
		t.Fatal(err)
	}
	if err := mirror.ConfigureRepository("mirror", "uploadpack.allowFilter", "true"); err != nil {
		t.Fatal(err)
	}
	if err := mirror.ConfigureRepository("mirror", "uploadpack.allowAnySHA1InWant", "true"); err != nil {
		t.Fatal(err)
	}

	e.Commit = commit
	e.Branch = "feature-branch"
	e.PullRequest = "false"
	e.GitRemoteMirrorURL = mirror.RepoURL("mirror")
	e.GitCloneFlags = "-v --filter=blob:none"

	if err := e.defaultCheckoutPhase(t.Context(), 0); err != nil {
		t.Fatalf("defaultCheckoutPhase() error = %v", err)
	}

	assertCheckedOutCommit(t, e, commit)
	assertOriginURL(t, e, e.Repository)
	if got, err := e.shell.Command("git", "config", "--local", "--get", "remote.origin.promisor").RunAndCaptureStdout(t.Context()); err != nil || got != "true" {
		t.Errorf("remote.origin.promisor = %q, %v; want true", got, err)
	}
	if got, err := e.shell.Command("git", "config", "--local", "--get", "remote.origin.partialclonefilter").RunAndCaptureStdout(t.Context()); err != nil || got != "blob:none" {
		t.Errorf("remote.origin.partialclonefilter = %q, %v; want blob:none", got, err)
	}
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

func TestRemoteMirrorFetchDoesNotSendCanonicalCloneHeaders(t *testing.T) {
	e := newRemoteMirrorTestExecutor(t)
	canonical, commit := setupCheckoutTestRepo(t, e, "canonical")
	mirror := copyRemoteMirrorRepository(t, canonical.RepoURL("canonical"), "mirror")

	mirrorURL, err := url.Parse(mirror.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(mirrorURL)
	var leakedRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if req.Header.Get("Authorization") != "" {
			leakedRequests.Add(1)
		}
		proxy.ServeHTTP(rw, req)
	}))
	t.Cleanup(server.Close)

	e.Commit = commit
	e.Branch = "feature-branch"
	e.PullRequest = "false"
	e.GitRemoteMirrorURL = server.URL + "/mirror.git"
	// Credentials supplied for the canonical clone must not reach the mirror.
	e.GitCloneFlags = `-v --config "http.extraHeader=Authorization: Bearer canonical-secret"`
	// Nor may equivalent ambient configuration bypass clone-config ordering.
	e.shell.Env.Set("GIT_CONFIG_COUNT", "1")
	e.shell.Env.Set("GIT_CONFIG_KEY_0", "http.extraHeader")
	e.shell.Env.Set("GIT_CONFIG_VALUE_0", "Authorization: Bearer ambient-secret")
	templateDir := t.TempDir()
	templateConfig := []byte("[http]\n\textraHeader = Authorization: Bearer template-secret\n")
	if err := os.WriteFile(filepath.Join(templateDir, "config"), templateConfig, 0o600); err != nil {
		t.Fatal(err)
	}
	e.shell.Env.Set("GIT_TEMPLATE_DIR", templateDir)
	canonical.Close()

	if err := e.defaultCheckoutPhase(t.Context(), 0); err != nil {
		t.Fatalf("defaultCheckoutPhase() error = %v", err)
	}

	assertCheckedOutCommit(t, e, commit)
	if got := leakedRequests.Load(); got != 0 {
		t.Errorf("mirror received %d requests carrying the canonical clone Authorization header, want 0", got)
	}
	// The value must still be persisted for checkout and canonical transport.
	got, err := e.shell.Command("git", "config", "--local", "--get-all", "http.extraHeader").RunAndCaptureStdout(t.Context())
	if err != nil {
		t.Fatalf("reading http.extraHeader: %v", err)
	}
	if want := "Authorization: Bearer canonical-secret"; got != want {
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

func TestRemoteMirrorSkipsReusedCanonicalCheckout(t *testing.T) {
	e := newRemoteMirrorTestExecutor(t)
	canonical, initialCommit := setupCheckoutTestRepo(t, e, "canonical")
	e.Commit = initialCommit
	e.Branch = "feature-branch"
	e.PullRequest = "false"

	if err := e.defaultCheckoutPhase(t.Context(), 0); err != nil {
		t.Fatalf("initial defaultCheckoutPhase() error = %v", err)
	}

	canonicalCommit := pushUniqueCommit(t, canonical, "canonical", "feature-branch", "canonical-only.txt", "canonical only")
	if canonicalCommit == initialCommit {
		t.Fatal("pushUniqueCommit did not create a new commit")
	}

	e.Commit = canonicalCommit
	// A reused checkout must not contact a mirror where canonical transport
	// configuration may already be persisted in .git/config.
	e.GitRemoteMirrorURL = "http://127.0.0.1:1/mirror.git"
	canonicalURL := e.Repository

	if err := e.defaultCheckoutPhase(t.Context(), 0); err != nil {
		t.Fatalf("reused defaultCheckoutPhase() error = %v", err)
	}

	assertCheckedOutCommit(t, e, canonicalCommit)
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

func TestRetargetRemoteMirrorPromisor(t *testing.T) {
	e := newRemoteMirrorTestExecutor(t)
	dir := t.TempDir()
	if err := e.shell.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	if err := e.shell.Command("git", "init").Run(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := e.shell.Command("git", "remote", "add", "origin", "https://canonical.example/repo.git").Run(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := e.shell.Command("git", "config", "remote.mirror.promisor", "true").Run(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := e.shell.Command("git", "config", "remote.mirror.partialclonefilter", "blob:none").Run(t.Context()); err != nil {
		t.Fatal(err)
	}

	if err := e.retargetRemoteMirrorPromisor(t.Context(), nil); err != nil {
		t.Fatal(err)
	}

	if got, err := e.shell.Command("git", "config", "--local", "--get", "remote.origin.promisor").RunAndCaptureStdout(t.Context()); err != nil || got != "true" {
		t.Errorf("remote.origin.promisor = %q, %v; want true", got, err)
	}
	if got, err := e.shell.Command("git", "config", "--local", "--get", "remote.origin.partialclonefilter").RunAndCaptureStdout(t.Context()); err != nil || got != "blob:none" {
		t.Errorf("remote.origin.partialclonefilter = %q, %v; want blob:none", got, err)
	}
	if _, err := e.shell.Command("git", "config", "--local", "--get", "remote.mirror.promisor").RunAndCaptureStdout(t.Context()); err == nil {
		t.Error("remote mirror promisor config remains")
	}
}

func TestPlanRemoteMirrorCheckout(t *testing.T) {
	t.Parallel()

	plan, ok, err := planRemoteMirrorCheckout(
		[]string{
			"-v",
			"--depth=50",
			"--filter=blob:none",
			"--sparse",
			"--config", "core.autocrlf=false",
			"--config", "fetch.uriProtocols=https",
		},
		"-v --prune --depth=50",
	)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected checkout configuration to support a remote mirror")
	}
	fetchFlags, err := shellwords.Split(plan.fetchFlags)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"-v", "--prune", "--depth=50", "--filter=blob:none", "--no-tags"}; !slices.Equal(fetchFlags, want) {
		t.Errorf("fetch flags = %q, want %q", fetchFlags, want)
	}
	if len(plan.cloneConfigs) != 2 {
		t.Fatalf("clone configs = %#v, want two values", plan.cloneConfigs)
	}
	if len(plan.mirrorConfigs) != 1 || plan.mirrorConfigs[0] != [2]string{"fetch.uriProtocols", "https"} {
		t.Fatalf("mirror configs = %#v, want fetch.uriProtocols=https", plan.mirrorConfigs)
	}

	// git clone overwrites its own remote config, but the mirror path would
	// add a second value, giving git push an extra destination.
	for _, flags := range [][]string{
		{"--config", "remote.origin.url=https://example.com/evil.git"},
		{"--config=remote.origin.url=https://example.com/evil.git"},
		{"--config", "REMOTE.origin.URL=https://example.com/evil.git"},
		{"--config", "remote.origin.pushurl=https://example.com/evil.git"},
		{"--config", "branch.main.remote=upstream"},
		{"--config", "branch.main.merge=refs/heads/other"},
	} {
		if _, ok, err := planRemoteMirrorCheckout(flags, "-v --prune"); err != nil {
			t.Errorf("planRemoteMirrorCheckout(%q) error = %v", flags, err)
		} else if ok {
			t.Errorf("planRemoteMirrorCheckout(%q) reported compatible, want incompatible", flags)
		}
	}

	// Transport-affecting configuration remains persisted for the canonical
	// repository but is not exposed to the mirror.
	plan, ok, err = planRemoteMirrorCheckout(
		[]string{"--config", "http.extraHeader=Authorization: Bearer canonical-secret"},
		"-v --prune",
	)
	if err != nil || !ok {
		t.Fatalf("planRemoteMirrorCheckout() = (%#v, %t, %v), want compatible", plan, ok, err)
	}
	if len(plan.mirrorConfigs) != 0 {
		t.Errorf("mirror configs = %#v, want no canonical credentials", plan.mirrorConfigs)
	}

	// Mirror network commands never receive arbitrary transport controls.
	if _, ok, err := planRemoteMirrorCheckout([]string{"-v"}, "-v --upload-pack=evil"); err != nil {
		t.Fatal(err)
	} else if ok {
		t.Fatal("expected upload-pack fetch flag to disable the remote mirror")
	}
	if _, ok, err := planRemoteMirrorCheckout([]string{"-v"}, "-v --tags"); err != nil {
		t.Fatal(err)
	} else if ok {
		t.Fatal("expected --tags to disable the remote mirror")
	}

	// Only HTTPS packfile URIs are accepted on the mirror trust boundary.
	if _, ok, err := planRemoteMirrorCheckout(
		[]string{"--config", "fetch.uriProtocols=http,https"},
		"-v --prune",
	); err != nil {
		t.Fatal(err)
	} else if ok {
		t.Fatal("expected non-HTTPS URI protocols to disable the remote mirror")
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
