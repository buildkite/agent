package job

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/buildkite/agent/v4/internal/experiments"
	"github.com/buildkite/agent/v4/tracetools"
	"github.com/buildkite/shellwords"
	"go.opentelemetry.io/otel/codes"
)

func TestGitAcceleratorCheckout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX Git launcher to trust the test certificate")
	}
	t.Setenv("BUILDKITE_TEST_BOOTSTRAP_ENV", "parent-only")
	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	originURL, err := url.Parse(canonical.URL)
	if err != nil {
		t.Fatal(err)
	}
	const origin = "https://git.example/acme/private.git"
	const repositoryPath = "/git.example/acme/private.git"
	var credentialRequests, gitRequests, redirectRequests atomic.Int32
	var redirect atomic.Bool
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		credentialRequests.Add(1)
		var input struct {
			RepoURL string `json:"repo_url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil || input.RepoURL != origin || !strings.HasSuffix(r.URL.Path, "/jobs/test-job/repository_access_token") {
			t.Errorf("unexpected repository credential request %s: %+v, %v", r.URL.Path, input, err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"username":"reader","token":"fixture-token"}`))
	}))
	t.Cleanup(backend.Close)
	proxy := httputil.NewSingleHostReverseProxy(originURL)
	accelerator := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, repositoryPath+"/") {
			redirectRequests.Add(1)
			http.NotFound(w, r)
			return
		}
		gitRequests.Add(1)
		username, password, ok := r.BasicAuth()
		if !ok || username != "reader" || password != "fixture-token" {
			t.Error("missing preemptive auth on accelerator Git request")
			http.NotFound(w, r) // No Basic challenge, as with some private providers.
			return
		}
		if redirect.Load() {
			http.Redirect(w, r, "/elsewhere", http.StatusTemporaryRedirect)
			return
		}
		r.Header.Del("Authorization")
		r.URL.Path = "/canonical.git" + strings.TrimPrefix(r.URL.Path, repositoryPath)
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(accelerator.Close)
	launcherDir := acceleratorTestGitLauncher(t, accelerator)

	commit, _, err := canonical.PushBranch("canonical", "feature-branch")
	if err != nil {
		t.Fatal(err)
	}
	e, _ := newFreshCloneRemoteMirrorExecutor(t, origin, "", commit)
	e.JobID = "test-job"
	e.GitAcceleratorURL, e.GitAcceleratorPath = accelerator.URL, launcherDir
	e.GitAcceleratorHosts = []string{"unrelated.example", "GIT.EXAMPLE"}
	e.GitCleanFlags = "-ffxdq"
	e.GitCommitVerification = GitCommitVerificationOff
	e.shell.Env.Set("BUILDKITE_USE_REPOSITORY_PROVIDER_GIT_CREDENTIALS", "true")
	e.shell.Env.Set("BUILDKITE_AGENT_ENDPOINT", backend.URL)
	// Only normal Git sees these: direct fallback uses the local fixture, and
	// accelerator Git must not use ambient Git authorization configuration.
	e.shell.Env.Set("GIT_CONFIG_COUNT", "2")
	e.shell.Env.Set("GIT_CONFIG_KEY_0", "url."+canonical.RepoURL("canonical")+".insteadOf")
	e.shell.Env.Set("GIT_CONFIG_VALUE_0", origin)
	e.shell.Env.Set("GIT_CONFIG_KEY_1", "http."+accelerator.URL+".extraHeader")
	e.shell.Env.Set("GIT_CONFIG_VALUE_1", "Authorization: Basic invalid-ambient-auth")
	ctx, _ := experiments.Enable(t.Context(), experiments.GitAcceleratorMirror)
	writer := t.TempDir()
	runGitForMirrorTest(t, "", "clone", "--branch", "feature-branch", canonical.RepoURL("canonical"), writer)
	for i, name := range []string{"fresh clone", "existing checkout fetch", "direct fallback", "host not listed"} {
		t.Run(name, func(t *testing.T) {
			if i > 0 {
				if err := os.WriteFile(filepath.Join(writer, "newfile.txt"), []byte(fmt.Sprint(i)), 0o600); err != nil {
					t.Fatal(err)
				}
				runGitForMirrorTest(t, writer, "commit", "-am", name)
				runGitForMirrorTest(t, writer, "push", "origin", "feature-branch")
				e.Commit = gitOutputForRemoteCheckoutTest(t, writer, "rev-parse", "HEAD")
			}
			redirect.Store(i == 2)
			if i == 3 {
				e.GitAcceleratorHosts = []string{"example", "git.example.other"}
			}
			before := gitRequests.Load()
			beforeCredentials := credentialRequests.Load()
			if err := e.defaultCheckoutPhase(ctx, 0); err != nil {
				t.Fatalf("checkout: %v", err)
			}
			gitCalls, credentialCalls := gitRequests.Load()-before, credentialRequests.Load()-beforeCredentials
			if i == 3 {
				if gitCalls != 0 || credentialCalls != 0 {
					t.Errorf("unlisted host used accelerator: Git requests %d, credential requests %d", gitCalls, credentialCalls)
				}
			} else if gitCalls == 0 || credentialCalls != 1 {
				t.Errorf("did not attempt authenticated accelerator Git: Git requests %d, credential requests %d", gitCalls, credentialCalls)
			}
			if redirectRequests.Load() != 0 {
				t.Error("accelerator Git followed a redirect")
			}
			assertGitConfigForRemoteMirrorTest(t, e.shell.Getwd(), "remote.origin.url", origin)
			if got := gitOutputForRemoteCheckoutTest(t, e.shell.Getwd(), "rev-parse", "HEAD"); got != e.Commit {
				t.Errorf("checked-out commit = %s, want %s", got, e.Commit)
			}
			if i > 0 && readRemoteMirrorTestFile(t, filepath.Join(e.shell.Getwd(), "newfile.txt")) != fmt.Sprint(i) {
				t.Errorf("requested revision did not materialize in the workspace")
			}
		})
	}
	t.Run("fresh clone preserves custom fetch flags", func(t *testing.T) {
		if err := e.removeCheckoutDir(); err != nil {
			t.Fatal(err)
		}
		e.GitAcceleratorHosts = []string{"git.example"}
		e.GitFetchFlags = "--depth=1"
		before := gitRequests.Load()
		if err := e.defaultCheckoutPhase(ctx, 0); err != nil {
			t.Fatal(err)
		}
		if gitRequests.Load() != before {
			t.Error("custom fetch flags should bypass the accelerator")
		}
		if got := gitOutputForRemoteCheckoutTest(t, e.shell.Getwd(), "rev-parse", "--is-shallow-repository"); got != "true" {
			t.Errorf("checkout is shallow = %s, want true with --depth=1", got)
		}
	})
}

func acceleratorTestGitLauncher(t *testing.T, accelerator *httptest.Server) string {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	launcherDir := t.TempDir()
	certPath := filepath.Join(launcherDir, "ca.pem")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: accelerator.Certificate().Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	launcher := "#!/bin/sh\n[ -z \"${BUILDKITE_TEST_BOOTSTRAP_ENV+x}\" ] || exit 1\nexec " + shellwords.Quote(git) + " -c " + shellwords.Quote("http.sslCAInfo="+certPath) + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(launcherDir, "git"), []byte(launcher), 0o700); err != nil {
		t.Fatal(err)
	}
	return launcherDir
}

func TestGitAcceleratorCloneKitCheckout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX Git launcher to trust the test certificate")
	}
	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	canonicalURL := canonical.RepoURL("canonical")
	upstream, err := url.Parse(canonical.URL)
	if err != nil {
		t.Fatal(err)
	}
	writer := t.TempDir()
	runGitForMirrorTest(t, "", "clone", "--branch", "main", canonicalURL, writer)
	// Make the seed history large enough to distinguish a delta from a full
	// transfer, with deterministic data that does not compress to a few bytes.
	payload := make([]byte, 2<<20)
	if _, err := rand.NewChaCha8([32]byte{1}).Read(payload); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(writer, "payload.bin"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	runGitForMirrorTest(t, writer, "add", "payload.bin")
	runGitForMirrorTest(t, writer, "commit", "-m", "Seed commit")
	runGitForMirrorTest(t, writer, "push", "origin", "main")
	seedTip := gitOutputForRemoteCheckoutTest(t, writer, "rev-parse", "HEAD")
	manifest, files := makeCloneKitTestSeed(t, canonicalURL, seedTip)
	runGitForMirrorTest(t, writer, "tag", "at-seed")
	runGitForMirrorTest(t, writer, "push", "origin", "at-seed")
	runGitForMirrorTest(t, writer, "checkout", "-b", "feature-branch")
	runGitForMirrorTest(t, writer, "push", "origin", "feature-branch")
	stale := copyOnHostMirrorHTTPRepo(t, canonicalURL, "canonical")
	staleURL, err := url.Parse(stale.URL)
	if err != nil {
		t.Fatal(err)
	}
	staleProxy := httputil.NewSingleHostReverseProxy(staleURL)
	runGitForMirrorTest(t, writer, "commit", "--allow-empty", "-m", "Commit after seed")
	runGitForMirrorTest(t, writer, "tag", "after-seed")
	runGitForMirrorTest(t, writer, "push", "origin", "feature-branch", "after-seed")
	commit := gitOutputForRemoteCheckoutTest(t, writer, "rev-parse", "HEAD")
	advanced := copyOnHostMirrorHTTPRepo(t, canonicalURL, "canonical")
	advancedURL, err := url.Parse(advanced.URL)
	if err != nil {
		t.Fatal(err)
	}
	runGitForMirrorTest(t, writer, "commit", "--allow-empty", "-m", "Branch moved after HEAD lookup")
	runGitForMirrorTest(t, writer, "push", advanced.RepoURL("canonical"), "feature-branch", ":refs/tags/after-seed")
	advancedProxy := httputil.NewSingleHostReverseProxy(advancedURL)

	const origin = "https://git.example/acme/private.git"
	const repo = "/git.example/acme/private.git"
	var manifestRequests, credentialRequests, gitRequests, canonicalRequests atomic.Int32
	var headDiscoveries atomic.Int32
	var gitBytes, seedBytes atomic.Int64
	var cancelClone atomic.Pointer[context.CancelFunc]
	var mode atomic.Value
	mode.Store("mirror-only")
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		credentialRequests.Add(1)
		var input struct {
			RepoURL string `json:"repo_url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil || input.RepoURL != origin {
			t.Error("managed credential was not requested for the canonical repository")
		}
		if mode.Load() == "token-unavailable" {
			http.Error(w, "cannot issue token", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"username":"reader","token":"fixture-token"}`)
	}))
	t.Cleanup(backend.Close)
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	canonicalProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		canonicalRequests.Add(1)
		if r.Header.Get("Authorization") != "" {
			t.Error("accelerator credential reached canonical Git")
		}
		if mode.Load() == "head-seed-fetch-fails" {
			advancedProxy.ServeHTTP(w, r)
			return
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(canonicalProxy.Close)
	foreignServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("CloneKit contacted a different HTTPS authority")
		http.NotFound(w, r)
	}))
	t.Cleanup(foreignServer.Close)
	accelerator := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state := mode.Load().(string)
		if (state == "anonymous" || state == "token-unavailable") && r.Header.Get("Authorization") != "" {
			t.Error("anonymous checkout sent authorization")
		}
		if kind, ok := strings.CutPrefix(r.URL.Path, repo+"/clone-kit/files/"); ok {
			if r.Header.Get("Authorization") != "" || r.URL.Query().Get("token") != "test-signed-url" {
				t.Error("artifact request did not use only the signed URL")
			}
			data := files[filepath.Base(kind)]
			if state == "expired" {
				http.NotFound(w, r)
				return
			}
			if state == "corrupt" && strings.HasSuffix(kind, "/pack") {
				data = append([]byte(nil), data...)
				data[0] ^= 1
			}
			w.Header().Set("Content-Length", fmt.Sprint(len(data)))
			n, _ := w.Write(data)
			seedBytes.Add(int64(n))
			return
		}
		if state == "token-rejected" && r.Header.Get("Authorization") != "" {
			if r.URL.Path == repo+"/clone-kit" {
				manifestRequests.Add(1)
			}
			w.Header().Set("WWW-Authenticate", `Basic realm="git-accelerator"`)
			http.Error(w, "token rejected", http.StatusUnauthorized)
			return
		}
		if state != "anonymous" && state != "token-unavailable" && state != "token-rejected" {
			if u, p, ok := r.BasicAuth(); !ok || u != "reader" || p != "fixture-token" {
				t.Error("missing preemptive accelerator authorization")
				http.NotFound(w, r)
				return
			}
		}
		if r.URL.Path == repo+"/clone-kit" {
			manifestRequests.Add(1)
			if state == "cancel" {
				(*cancelClone.Load())()
				<-r.Context().Done()
				return
			}
			if state == "cold" {
				http.NotFound(w, r)
				return
			}
			m := manifest
			m.Files = append([]cloneKitFile(nil), m.Files...)
			for i, file := range m.Files {
				authority := r.Host
				if state == "foreign-file" {
					authority = strings.TrimPrefix(foreignServer.URL, "https://")
				}
				m.Files[i].URL = "https://" + authority + repo + "/clone-kit/files/" + m.packID + "/" + file.Name + "?token=test-signed-url"
			}
			_ = json.NewEncoder(w).Encode(m)
			return
		}
		gitRequests.Add(1)
		if state == "git-down" {
			http.Error(w, "Git unavailable", http.StatusServiceUnavailable)
			return
		}
		gitProxy := proxy
		if strings.HasPrefix(state, "head-") {
			if strings.HasSuffix(r.URL.Path, "/info/refs") {
				headDiscoveries.Add(1)
			}
			first := headDiscoveries.Load() == 1
			switch {
			case state == "head-seed-lookup-fails" && first:
				http.Error(w, "Git unavailable", http.StatusServiceUnavailable)
				return
			case state == "head-seed-fetch-fails" && !first:
				http.Error(w, "Git unavailable", http.StatusServiceUnavailable)
				return
			case state == "head-seed-miss" && !first:
				gitProxy = advancedProxy
			case strings.HasPrefix(state, "head-seed-"):
				// Early resolution sees the normal mirror tip.
			case first:
				// The follow-up fetch sees a newer branch tip than the clone did.
				gitProxy = staleProxy
			case state == "head-fetch-down":
				http.Error(w, "Git unavailable", http.StatusServiceUnavailable)
				return
			}
		}
		r.Header.Del("Authorization")
		r.URL.Path = "/canonical.git" + strings.TrimPrefix(r.URL.Path, repo)
		if r.Method == http.MethodPost {
			gitProxy.ServeHTTP(&acceleratorByteCounter{ResponseWriter: w, bytes: &gitBytes}, r)
			return
		}
		gitProxy.ServeHTTP(w, r)
	}))
	t.Cleanup(accelerator.Close)
	launcher := acceleratorTestGitLauncher(t, accelerator)
	// No parallelism: substitute the default TLS roots for the test server.
	oldTransport := http.DefaultTransport
	http.DefaultTransport = accelerator.Client().Transport
	defer func() { http.DefaultTransport = oldTransport }()

	for _, bare := range []bool{false, true} {
		cases := []string{"mirror-only", "hit"}
		if !bare {
			cases = append(cases, "seed-only-hit", "seed-only-miss", "head-seed-hit", "head-seed-miss", "head-seed-lookup-fails", "head-seed-fetch-fails", "head-mirror", "head-clonekit", "head-fetch-down", "cold", "expired", "corrupt", "foreign-file", "git-down", "host-excluded", "anonymous", "token-unavailable", "token-rejected", "job-tls-roots", "cancel")
		}
		for _, state := range cases {
			t.Run(fmt.Sprintf("bare=%t/%s", bare, state), func(t *testing.T) {
				mode.Store(state)
				e, _ := newFreshCloneRemoteMirrorExecutor(t, origin, "", commit)
				head := strings.HasPrefix(state, "head-")
				if head {
					e.Commit = "HEAD"
				}
				e.JobID = "test-job"
				e.GitAcceleratorURL, e.GitAcceleratorPath = accelerator.URL, launcher
				e.GitAcceleratorDisableHTTP = state == "job-tls-roots"
				e.GitCleanFlags, e.GitCommitVerification = "-ffxdq", GitCommitVerificationOff
				if state == "seed-only-hit" {
					e.Commit, e.Branch = seedTip, "main"
					e.GitCommitVerification = GitCommitVerificationFailClosed
				}
				if state == "head-seed-hit" {
					e.Branch = "main"
				}
				e.shell.Env.Set("BUILDKITE_AGENT_ENDPOINT", backend.URL)
				e.shell.Env.Set("GIT_CONFIG_COUNT", "1")
				e.shell.Env.Set("GIT_CONFIG_KEY_0", "url."+canonicalProxy.URL+"/canonical.git.insteadOf")
				e.shell.Env.Set("GIT_CONFIG_VALUE_0", origin)
				if state != "anonymous" {
					e.shell.Env.Set("BUILDKITE_USE_REPOSITORY_PROVIDER_GIT_CREDENTIALS", "true")
				}
				if state == "host-excluded" {
					e.GitAcceleratorHosts = []string{"other.example"}
				}
				seedParent := filepath.Dir(e.shell.Getwd())
				if bare {
					e.GitMirrorsPath, e.GitMirrorsLockTimeout, e.GitMirrorCheckoutMode = t.TempDir(), 30, "dissociate"
					seedParent = e.GitMirrorsPath
				}
				t.Cleanup(func() {
					if matches, err := filepath.Glob(filepath.Join(seedParent, "buildkite-clonekit-*")); err != nil || len(matches) != 0 {
						t.Errorf("temporary seed directories remain: %v, err=%v", matches, err)
					}
				})
				ctx, _ := experiments.Enable(t.Context(), experiments.GitAcceleratorMirror)
				if state != "mirror-only" && state != "head-mirror" {
					ctx, _ = experiments.Enable(ctx, experiments.GitAcceleratorCloneKit)
				}
				if strings.HasPrefix(state, "seed-only-") || strings.HasPrefix(state, "head-seed-") {
					ctx, _ = experiments.Enable(ctx, experiments.GitAcceleratorCloneKitSeedOnly)
				}
				ctx, cancel := context.WithCancel(ctx)
				defer cancel()
				cancelClone.Store(&cancel)
				gitBytes.Store(0)
				seedBytes.Store(0)
				manifestRequests.Store(0)
				credentialRequests.Store(0)
				gitRequests.Store(0)
				canonicalRequests.Store(0)
				headDiscoveries.Store(0)
				recorder := installGlobalSpanRecorder(t)
				e.TracingBackend = tracetools.BackendOpenTelemetry
				started := time.Now()
				err := e.defaultCheckoutPhase(ctx, 0)
				if state == "cancel" {
					if !errors.Is(err, context.Canceled) || manifestRequests.Load() != 1 || gitRequests.Load() != 0 || canonicalRequests.Load() != 0 {
						t.Fatalf("cancelled checkout: err=%v, manifest=%d, Git=%d, canonical=%d", err, manifestRequests.Load(), gitRequests.Load(), canonicalRequests.Load())
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if !bare && state == "hit" {
					spans := recorder.Ended()
					for _, name := range []string{"git.accelerator.credentials", "git.accelerator.clonekit", "git.clone.command", "git.accelerator.clonekit.install"} {
						assertSpanChildOf(t, spans, name, "git.clone")
					}
					for _, stage := range []string{"manifest", "download", "verify"} {
						assertSpanChildOf(t, spans, "git.accelerator.clonekit."+stage, "git.accelerator.clonekit")
					}
					command := findOnlySpan(t, spans, "git.clone.command")
					assertSpanAttr(t, command, "git.clone.source", "git-accelerator")
					assertSpanAttr(t, command, "git.clone.seeded", "true")
				}
				if state == "token-unavailable" {
					span := findOnlySpan(t, recorder.Ended(), "git.accelerator.credentials")
					if got := span.Status().Code; got != codes.Unset {
						t.Errorf("credential fallback span status = %v, want Unset", got)
					}
					assertSpanAttr(t, span, "outcome", "anonymous-fallback")
				}
				t.Logf("checkout: %s; Git response bytes: %d; seed file bytes: %d", time.Since(started), gitBytes.Load(), seedBytes.Load())
				if (state == "mirror-only" || state == "head-mirror" || state == "job-tls-roots") && gitBytes.Load() < int64(len(payload)) {
					t.Fatal("mirror-only control did not transfer the full history")
				}
				switch state {
				case "seed-only-hit":
					if gitRequests.Load() != 0 || seedBytes.Load() < int64(len(payload)) {
						t.Fatal("checkout did not use only the seed for the pinned commit")
					}
				case "head-seed-hit":
					if headDiscoveries.Load() != 1 || seedBytes.Load() < int64(len(payload)) {
						t.Fatal("HEAD seed hit should only look up the branch through Git")
					}
				case "hit", "seed-only-miss", "head-seed-miss", "head-clonekit", "anonymous", "token-unavailable", "token-rejected":
					if gitBytes.Load() <= 0 || gitBytes.Load() >= int64(len(payload))/10 || seedBytes.Load() < int64(len(payload)) {
						t.Fatal("seed did not replace the bulk of the Git transfer")
					}
				case "cold", "expired", "corrupt", "foreign-file":
					if gitBytes.Load() < int64(len(payload)) || canonicalRequests.Load() != 0 {
						t.Fatal("unusable seed did not fall back to unseeded accelerator Git")
					}
				}
				// A workspace with an on-host mirror still uses canonical origin
				// with a reference, so its canonical requests are not fallback.
				// The seed-only hit performs normal branch verification at origin.
				if !bare && (state == "seed-only-hit" || state == "head-fetch-down" || state == "head-seed-fetch-fails" || state == "git-down" || state == "host-excluded") != (canonicalRequests.Load() > 0) {
					t.Errorf("unexpected canonical request count %d", canonicalRequests.Load())
				}
				if head && state != "head-seed-hit" && headDiscoveries.Load() < 2 {
					t.Error("HEAD fetch did not try the accelerator after cloning")
				}
				var wantManifests int32 = 1
				switch state {
				case "mirror-only", "head-mirror", "host-excluded", "job-tls-roots":
					wantManifests = 0
				case "token-rejected":
					wantManifests = 2 // The original request carries the token; the retry does not.
				}
				if got := manifestRequests.Load(); got != wantManifests {
					t.Errorf("manifest requests = %d, want %d", got, wantManifests)
				}
				wantCredentials := state != "host-excluded" && state != "anonymous"
				if (wantCredentials && credentialRequests.Load() != 1) || (!wantCredentials && credentialRequests.Load() != 0) {
					t.Errorf("credential requests = %d", credentialRequests.Load())
				}
				work := e.shell.Getwd()
				assertGitConfigForRemoteMirrorTest(t, work, "remote.origin.url", origin)
				if state == "seed-only-hit" || state == "head-seed-hit" || state == "head-seed-miss" {
					wantCommit, wantPacks := seedTip, 1
					if state == "head-seed-miss" {
						wantCommit, wantPacks = commit, 2
					}
					if got := gitOutputForRemoteCheckoutTest(t, work, "rev-parse", "HEAD"); got != wantCommit {
						t.Fatalf("HEAD = %s, want %s", got, wantCommit)
					}
					if got := gitOutputForRemoteCheckoutTest(t, work, "for-each-ref", "refs/remotes/origin/feature-branch", "refs/tags"); got != "" {
						t.Errorf("unexpected branch or tags: %s", got)
					}
					if packs, err := filepath.Glob(filepath.Join(work, ".git", "objects", "pack", "*.pack")); err != nil || len(packs) != wantPacks {
						t.Errorf("expected %d packs, got %v, err=%v", wantPacks, packs, err)
					}
					if got, err := os.ReadFile(filepath.Join(work, "payload.bin")); err != nil || string(got) != string(payload) {
						t.Errorf("seed-only checkout did not materialize the expected payload: %v", err)
					}
				} else if state == "head-seed-fetch-fails" {
					if got := gitOutputForRemoteCheckoutTest(t, work, "rev-parse", "HEAD"); got != commit {
						t.Fatalf("canonical fallback changed resolved HEAD: got %s, want %s", got, commit)
					}
				} else if head {
					if got := gitOutputForRemoteCheckoutTest(t, work, "rev-parse", "HEAD", "FETCH_HEAD", "refs/remotes/origin/feature-branch"); got != strings.Join([]string{commit, commit, commit}, "\n") {
						t.Fatalf("checkout did not use the refreshed branch tip %s: %s", commit, got)
					}
				} else if got := gitOutputForRemoteCheckoutTest(t, work, "rev-parse", "HEAD", "refs/remotes/origin/feature-branch", "refs/tags/after-seed"); got != strings.Join([]string{commit, commit, commit}, "\n") {
					t.Fatalf("wrong checkout or refs: %s", got)
				}
				gitOutputForRemoteCheckoutTest(t, work, "fsck", "--full", "--no-dangling")
				dirs := []string{filepath.Join(work, ".git")}
				if bare {
					dir := expectedOnHostMirrorDir(e)
					dirs = append(dirs, dir)
					if got := gitOutputForMirrorTest(t, dir, "config", "remote.origin.url"); got != origin {
						t.Errorf("mirror origin = %q, want %q", got, origin)
					}
					if got := gitOutputForMirrorTest(t, dir, "rev-parse", "refs/heads/feature-branch", "refs/tags/after-seed"); got != commit+"\n"+commit {
						t.Errorf("wrong mirror refs: %s", got)
					}
					gitOutputForMirrorTest(t, dir, "fsck", "--full", "--no-dangling")
				}
				for _, dir := range dirs {
					if _, err := os.Stat(filepath.Join(dir, "objects", "info", "alternates")); !os.IsNotExist(err) {
						t.Errorf("temporary alternate survived in %s: %v", dir, err)
					}
				}
				if state == "hit" || state == "seed-only-hit" || state == "head-clonekit" || state == "head-fetch-down" {
					for _, file := range manifest.Files {
						path := filepath.Join(dirs[len(dirs)-1], file.Path)
						contents, err := os.ReadFile(path)
						if err != nil {
							t.Fatalf("verified seed file was not retained: %v", err)
						}
						if cloneKitTestDigest(string(contents)) != file.SHA256 {
							t.Errorf("seed file %s was changed during clone", file.Name)
						}
					}
				}
				if !bare && state == "hit" {
					if err := e.defaultCheckoutPhase(ctx, 0); err != nil {
						t.Fatal(err)
					}
					if manifestRequests.Load() != 1 {
						t.Error("existing checkout requested another seed")
					}
				}
			})
		}
	}
}

type acceleratorByteCounter struct {
	http.ResponseWriter
	bytes *atomic.Int64
}

func (w *acceleratorByteCounter) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	w.bytes.Add(int64(n))
	return n, err
}

func TestGitAcceleratorRouting(t *testing.T) {
	e, _ := newFreshCloneRemoteMirrorExecutor(t, "https://git.example/group/nested/project.git", "", strings.Repeat("a", 40))
	e.GitAcceleratorURL = "https://cache.example:8443/"
	ctx, _ := experiments.Enable(t.Context(), experiments.GitAcceleratorMirror)
	const want = "https://cache.example:8443/git.example/group/nested/project.git"
	if got := e.resolveRemoteMirrorAttempt(ctx, 0); !got.accelerator || got.url != want {
		t.Errorf("route = %+v, want accelerator URL %s", got, want)
	}
	if got := e.resolveRemoteMirrorAttempt(t.Context(), 0); got.url != "" {
		t.Errorf("routed to %s without opt-in", got.url)
	}
	e.AllowedRepositories = []*regexp.Regexp{regexp.MustCompile(`^https://git\.example/`)}
	if got := e.resolveRemoteMirrorAttempt(ctx, 0); got.skipReason != remoteMirrorSkipAcceleratorPolicy {
		t.Errorf("route = %+v, want accelerator allowlist skip", got)
	}
	e.AllowedRepositories = append(e.AllowedRepositories, regexp.MustCompile(`^https://cache\.example:8443/git\.example/`))
	if got := e.resolveRemoteMirrorAttempt(ctx, 0); got.url != want {
		t.Errorf("route = %+v, want both allowlisted URLs to pass", got)
	}
	e.GitAcceleratorHosts = []string{"example"}
	e.GitRemoteMirrorURL = "https://explicit.example/repo.git"
	if got := e.resolveRemoteMirrorAttempt(ctx, 0); got.accelerator || got.url != e.GitRemoteMirrorURL {
		t.Errorf("route = %+v, want explicit backend mirror", got)
	}
}
