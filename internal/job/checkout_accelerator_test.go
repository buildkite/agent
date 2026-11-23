package job

import (
	"encoding/json"
	"encoding/pem"
	"fmt"
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

	"github.com/buildkite/agent/v4/internal/experiments"
	"github.com/buildkite/shellwords"
)

func TestGitAcceleratorCheckout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX Git launcher to trust the test certificate")
	}
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
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	launcherDir := t.TempDir()
	certPath := filepath.Join(launcherDir, "ca.pem")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: accelerator.Certificate().Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	launcher := "#!/bin/sh\nexec " + shellwords.Quote(git) + " -c " + shellwords.Quote("http.sslCAInfo="+certPath) + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(launcherDir, "git"), []byte(launcher), 0o700); err != nil {
		t.Fatal(err)
	}

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
