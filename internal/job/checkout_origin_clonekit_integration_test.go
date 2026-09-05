package job

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/buildkite/agent/v4/internal/osutil"
	"github.com/buildkite/agent/v4/internal/self"
	"github.com/buildkite/agent/v4/internal/shell"
)

type cloneKitTestTransport func(*http.Request) (*http.Response, error)

func (f cloneKitTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOriginCloneKitMirrorLifecycle(t *testing.T) {
	// No parallelism: substitute the default HTTP transport only for this test.
	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	commit, _, err := canonical.PushBranch("canonical", "feature-branch")
	if err != nil {
		t.Fatal(err)
	}
	packRepo := filepath.Join(t.TempDir(), "pack.git")
	runGitForMirrorTest(t, "", "clone", "--mirror", canonical.RepoURL("canonical"), packRepo)
	runGitForMirrorTest(t, packRepo, "-c", "pack.writeReverseIndex=true", "repack", "-ad")
	m := validCloneKitManifest()
	m.TipCommit, m.PackChunks, m.Files = commit, nil, nil
	data := make(map[string][]byte)
	entries, err := os.ReadDir(filepath.Join(packRepo, "objects", "pack"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := strings.TrimPrefix(filepath.Ext(entry.Name()), ".")
		if name != "pack" && name != "idx" && name != "rev" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(packRepo, "objects", "pack", entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		data[name] = b
		m.Files = append(m.Files, cloneKitFile{Name: name, Path: "objects/pack/" + entry.Name(), Size: int64(len(b)), SHA256: cloneKitTestDigest(string(b)), URL: "https://cdn.example/" + name + "?secret=presigned-secret"})
	}
	if err := m.validate(); err != nil {
		t.Fatal(err)
	}
	helper := filepath.Join(t.TempDir(), "helper")
	if err := os.WriteFile(helper, []byte("#!/bin/sh\ncat > \"$CLONEKIT_CREDENTIAL_INPUT\"\nprintf 'username=test-user\\npassword=repository-secret\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"hit", "miss", "corrupt", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(self.OverridePath(t.Context(), helper))
			defer cancel()
			e := newOnHostMirrorExecutor(t, canonical.RepoURL("canonical"), commit)
			e.CleanCheckout = true
			e.Phases = []string{"checkout", "command"}
			input := filepath.Join(t.TempDir(), "input")
			e.shell.Env.Set("CLONEKIT_CREDENTIAL_INPUT", input)
			var logs bytes.Buffer
			e.shell, err = shell.New(shell.WithEnv(e.shell.Env), shell.WithDebug(true), shell.WithStdout(&logs), shell.WithLogger(shell.NewWriterLogger(&logs, false, nil)))
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/clone-kit") {
					user, password, ok := r.BasicAuth()
					if !ok || user != "test-user" || password != "repository-secret" {
						t.Error("manifest auth missing")
					}
					if mode == "cancel" {
						cancel()
						<-r.Context().Done()
						return
					}
					if mode == "miss" {
						http.NotFound(w, r)
						return
					}
					_ = json.NewEncoder(w).Encode(m)
					return
				}
				if r.Header.Get("Authorization") != "" {
					t.Error("repository auth leaked to artifact")
				}
				b := bytes.Clone(data[strings.TrimPrefix(r.URL.Path, "/")])
				if mode == "corrupt" && len(b) > 0 {
					b[0] ^= 1
				}
				w.Header().Set("Content-Length", fmt.Sprint(len(b)))
				_, _ = w.Write(b)
			}))
			defer server.Close()
			old := http.DefaultTransport
			http.DefaultTransport = cloneKitTestTransport(func(r *http.Request) (*http.Response, error) {
				c := r.Clone(r.Context())
				c.URL.Host = strings.TrimPrefix(server.URL, "https://")
				return server.Client().Transport.RoundTrip(c)
			})
			defer func() { http.DefaultTransport = old }()
			attempt := remoteMirrorAttempt{site: remoteMirrorSiteOnHostMirror, url: "https://must-not-be-used.example/repo.git", cloneKitURL: "https://origin.cursor.com/acme/repo.git"}
			dir, err := e.updateGitMirror(ctx, e.Repository, &attempt)
			if mode == "cancel" {
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("error=%v, want cancellation", err)
				}
				if _, err := os.Stat(expectedOnHostMirrorDir(e)); !os.IsNotExist(err) {
					t.Fatal("canonical fallback started on cancellation")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				gitOutputForMirrorTest(t, dir, "fsck", "--full", "--no-dangling")
				checkout := filepath.Join(t.TempDir(), "checkout")
				runGitForMirrorTest(t, "", "clone", "--reference", dir, e.Repository, checkout)
				runGitForMirrorTest(t, checkout, "checkout", "--force", commit)
				gitOutputForRemoteCheckoutTest(t, checkout, "fsck", "--full", "--no-dangling")
				if gitOutputForRemoteCheckoutTest(t, checkout, "rev-parse", "HEAD") != commit {
					t.Fatal("wrong checkout commit")
				}
				if got := gitOutputForMirrorTest(t, expectedOnHostMirrorDir(e), "config", "remote.origin.url"); got != e.Repository {
					t.Fatal("canonical origin not preserved")
				}
				if mode == "hit" {
					if gitOutputForMirrorTest(t, dir, "rev-parse", cloneKitAnchor) != commit {
						t.Fatal("snapshot lost anchor")
					}
					if gitOutputForMirrorTest(t, dir, "symbolic-ref", "HEAD") != "refs/heads/main" {
						t.Fatal("invalid HEAD")
					}
					if runtime.GOOS != "windows" {
						for _, f := range m.Files {
							info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(f.Path)))
							if err != nil {
								t.Fatal(err)
							}
							if info.Mode().Perm() != 0o444&^osutil.Umask {
								t.Fatal("pack permissions do not respect shared-cache umask")
							}
						}
					}
				}
			}
			if _, err := os.Stat(remoteMirrorStagingDir(expectedOnHostMirrorDir(e))); !os.IsNotExist(err) {
				t.Fatal("staging remains")
			}
			for _, secret := range []string{"repository-secret", "presigned-secret"} {
				if strings.Contains(logs.String(), secret) {
					t.Fatal("secret logged")
				}
			}
			b, err := os.ReadFile(input)
			if err != nil || !strings.Contains(string(b), "path=acme/repo.git\n") || strings.Contains(string(b), "clone-kit") {
				t.Fatal("wrong credential target")
			}
		})
	}
}
