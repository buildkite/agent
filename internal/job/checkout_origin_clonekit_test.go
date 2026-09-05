package job

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/buildkite/agent/v4/internal/experiments"
	"github.com/buildkite/agent/v4/internal/shell"
)

const cloneKitTestPackID = "0123456789abcdef0123456789abcdef01234567"

func cloneKitTestDigest(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func validCloneKitManifest() cloneKitManifest {
	files := make([]cloneKitFile, 0, 3)
	for _, name := range []string{"pack", "idx", "rev"} {
		files = append(files, cloneKitFile{
			Name: name, Path: "objects/pack/pack-" + cloneKitTestPackID + "." + name,
			URL: "https://objects.example/" + name, Size: 1, SHA256: cloneKitTestDigest(name),
		})
	}
	return cloneKitManifest{
		Version: 1, RepoID: "123e4567-e89b-12d3-a456-426614174000",
		TipCommit: strings.Repeat("a", 40), Files: files,
		PackChunks: &cloneKitChunks{ChunkSize: 1, SHA256s: []string{cloneKitTestDigest("pack")}},
	}
}

func TestCloneKitManifestValidate(t *testing.T) {
	tests := map[string]func(*cloneKitManifest){
		"version":          func(m *cloneKitManifest) { m.Version = 2 },
		"uuid":             func(m *cloneKitManifest) { m.RepoID = "not-a-uuid" },
		"tip SHA1":         func(m *cloneKitManifest) { m.TipCommit = strings.Repeat("A", 40) },
		"missing file":     func(m *cloneKitManifest) { m.Files = m.Files[:2] },
		"duplicate kind":   func(m *cloneKitManifest) { m.Files[2].Name = "idx" },
		"mismatched SHA1":  func(m *cloneKitManifest) { m.Files[2].Path = "objects/pack/pack-" + strings.Repeat("b", 40) + ".rev" },
		"wrong path kind":  func(m *cloneKitManifest) { m.Files[2].Path = "objects/pack/pack-" + cloneKitTestPackID + ".idx" },
		"unsafe path":      func(m *cloneKitManifest) { m.Files[0].Path = "../pack-" + cloneKitTestPackID + ".pack" },
		"zero size":        func(m *cloneKitManifest) { m.Files[0].Size = 0 },
		"oversize sidecar": func(m *cloneKitManifest) { m.Files[1].Size = (4 << 30) + 1 },
		"oversize pack":    func(m *cloneKitManifest) { m.Files[0].Size = (64 << 30) + 1 },
		"digest":           func(m *cloneKitManifest) { m.Files[0].SHA256 = strings.Repeat("A", 64) },
		"URL scheme":       func(m *cloneKitManifest) { m.Files[0].URL = "http://objects.example/pack" },
		"URL credentials":  func(m *cloneKitManifest) { m.Files[0].URL = "https://secret@objects.example/pack" },
		"URL fragment":     func(m *cloneKitManifest) { m.Files[0].URL += "#fragment" },
		"chunk size":       func(m *cloneKitManifest) { m.PackChunks.ChunkSize = 0 },
		"chunk count":      func(m *cloneKitManifest) { m.PackChunks.SHA256s = nil },
		"chunk digest":     func(m *cloneKitManifest) { m.PackChunks.SHA256s[0] = "bad" },
	}
	m := validCloneKitManifest()
	if err := m.validate(); err != nil || m.packID != cloneKitTestPackID {
		t.Fatalf("valid manifest: error = %v, packID = %q", err, m.packID)
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			m := validCloneKitManifest()
			mutate(&m)
			if err := m.validate(); err == nil {
				t.Fatal("validate() error = nil, want invalid manifest")
			}
		})
	}
}

func TestOriginCloneKitSource(t *testing.T) {
	ctx, _ := experiments.Enable(t.Context(), experiments.OriginCloneKit)
	canonical := "https://origin.cursor.com/acme/repo.git"
	newExecutor := func() *Executor {
		sh, err := shell.New()
		if err != nil {
			t.Fatal(err)
		}
		return &Executor{ExecutorConfig: ExecutorConfig{Repository: canonical, GitMirrorsPath: t.TempDir()}, canonicalRepository: canonical, shell: sh}
	}
	if got := newExecutor().originCloneKitSource(ctx, 0); got != canonical {
		t.Fatalf("eligible source = %q, want %q", got, canonical)
	}

	tests := map[string]func(*Executor) (context.Context, int){
		"disabled":          func(e *Executor) (context.Context, int) { return t.Context(), 0 },
		"later attempt":     func(e *Executor) (context.Context, int) { return ctx, 1 },
		"no mirrors":        func(e *Executor) (context.Context, int) { e.GitMirrorsPath = ""; return ctx, 0 },
		"skip update":       func(e *Executor) (context.Context, int) { e.GitMirrorsSkipUpdate = true; return ctx, 0 },
		"clone flags":       func(e *Executor) (context.Context, int) { e.GitCloneMirrorFlags = "--depth=1"; return ctx, 0 },
		"canonical changed": func(e *Executor) (context.Context, int) { e.Repository += "?changed"; return ctx, 0 },
		"wrong host": func(e *Executor) (context.Context, int) {
			e.Repository = "https://evil.example/acme/repo.git"
			e.canonicalRepository = e.Repository
			return ctx, 0
		},
		"host suffix": func(e *Executor) (context.Context, int) {
			e.Repository = "https://origin.cursor.com.evil/acme/repo.git"
			e.canonicalRepository = e.Repository
			return ctx, 0
		},
		"SSH": func(e *Executor) (context.Context, int) {
			e.Repository = "git@origin.cursor.com:acme/repo.git"
			e.canonicalRepository = e.Repository
			return ctx, 0
		},
		"reuse": func(e *Executor) (context.Context, int) {
			checkout := t.TempDir()
			if err := os.Mkdir(filepath.Join(checkout, ".git"), 0o755); err != nil {
				t.Fatal(err)
			}
			e.shell.Env.Set("BUILDKITE_BUILD_CHECKOUT_PATH", checkout)
			return ctx, 0
		},
	}
	for name, alter := range tests {
		t.Run(name, func(t *testing.T) {
			e := newExecutor()
			gotCtx, attempt := alter(e)
			if got := e.originCloneKitSource(gotCtx, attempt); got != "" {
				t.Errorf("source = %q, want empty", got)
			}
		})
	}

	e := newExecutor()
	e.Repository, e.canonicalRepository = "git@github.com:acme/repo.git", "git@github.com:acme/repo.git"
	e.GitRemoteMirrorURL = canonical
	if got := e.originCloneKitSource(ctx, 0); got != canonical {
		t.Errorf("SSH canonical with HTTPS mirror = %q, want %q", got, canonical)
	}
}

func TestCloneKitRangeCount(t *testing.T) {
	for _, test := range []struct {
		size int64
		want int
	}{
		{1, 1}, {(64 << 20) - 1, 1}, {64 << 20, 8}, {65 << 20, 8}, {256 << 20, 32}, {64 << 30, 32},
	} {
		if got := cloneKitRangeCount(test.size); got != test.want {
			t.Errorf("cloneKitRangeCount(%d) = %d, want %d", test.size, got, test.want)
		}
	}
}

func TestDownloadCloneKitFileRangesAndAuthentication(t *testing.T) {
	const size = int64(64 << 20)
	var mu sync.Mutex
	var ranges []string
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "" || r.Header.Get("Accept-Encoding") != "identity" {
			t.Errorf("request method/auth/encoding = %q/%q/%q", r.Method, r.Header.Get("Authorization"), r.Header.Get("Accept-Encoding"))
		}
		var start, end int64
		if _, err := fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end); err != nil {
			t.Errorf("Range = %q: %v", r.Header.Get("Range"), err)
			return
		}
		mu.Lock()
		ranges = append(ranges, r.Header.Get("Range"))
		mu.Unlock()
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, size))
		w.Header().Set("Content-Length", fmt.Sprint(end-start+1))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = io.CopyN(w, zeroReader{}, end-start+1)
	}))
	t.Cleanup(s.Close)
	path := filepath.Join(t.TempDir(), "pack")
	if err := downloadCloneKitFile(t.Context(), s.Client(), cloneKitFile{Name: "pack", URL: s.URL, Size: size}, path); err != nil {
		t.Fatal(err)
	}
	wantRanges := make(map[string]bool)
	for i := int64(0); i < 8; i++ {
		wantRanges[fmt.Sprintf("bytes=%d-%d", i*(8<<20), (i+1)*(8<<20)-1)] = true
	}
	for _, got := range ranges {
		delete(wantRanges, got)
	}
	if len(ranges) != 8 || len(wantRanges) != 0 {
		t.Errorf("ranges = %v", ranges)
	}
	if info, err := os.Stat(path); err != nil || info.Size() != size {
		t.Errorf("download size: info=%v error=%v", info, err)
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }

func TestDownloadCloneKitFileRejectsResponses(t *testing.T) {
	tests := map[string]func(http.ResponseWriter){
		"status": func(w http.ResponseWriter) { w.WriteHeader(http.StatusPartialContent) },
		"length": func(w http.ResponseWriter) {
			w.Header().Set("Content-Length", "2")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("xx"))
		},
		"short body": func(w http.ResponseWriter) {
			w.Header().Set("Content-Length", "3")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("x"))
		},
	}
	for name, respond := range tests {
		t.Run(name, func(t *testing.T) {
			var requests atomic.Int32
			s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests.Add(1); respond(w) }))
			defer s.Close()
			err := downloadCloneKitFile(t.Context(), s.Client(), cloneKitFile{Name: "idx", URL: s.URL, Size: 3}, filepath.Join(t.TempDir(), "file"))
			if err == nil {
				t.Fatal("error = nil, want response rejection")
			}
			if requests.Load() != 1 {
				t.Errorf("requests = %d, want 1 (no retries)", requests.Load())
			}
		})
	}
}

func TestDownloadCloneKitFileCancellation(t *testing.T) {
	started := make(chan struct{})
	s := httptest.NewTLSServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done() }))
	defer s.Close()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- downloadCloneKitFile(ctx, s.Client(), cloneKitFile{Name: "idx", URL: s.URL, Size: 1}, filepath.Join(t.TempDir(), "file"))
	}()
	<-started
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("error = nil after cancellation")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("download did not observe cancellation")
	}
}

func TestVerifyCloneKitFileWholeAndChunks(t *testing.T) {
	data := []byte("abcdefgh")
	path := filepath.Join(t.TempDir(), "pack")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	file := cloneKitFile{Name: "pack", Size: int64(len(data)), SHA256: cloneKitTestDigest(string(data))}
	if err := verifyCloneKitFile(t.Context(), file, nil, path); err != nil {
		t.Fatalf("whole hash: %v", err)
	}
	chunks := &cloneKitChunks{ChunkSize: 3, SHA256s: []string{cloneKitTestDigest("abc"), cloneKitTestDigest("def"), cloneKitTestDigest("gh")}}
	if err := verifyCloneKitFile(t.Context(), file, chunks, path); err != nil {
		t.Fatalf("chunk hashes: %v", err)
	}
	chunks.SHA256s[1] = cloneKitTestDigest("bad")
	if err := verifyCloneKitFile(t.Context(), file, chunks, path); err == nil {
		t.Fatal("bad chunk hash accepted")
	}
	file.SHA256 = cloneKitTestDigest("bad")
	if err := verifyCloneKitFile(t.Context(), file, nil, path); err == nil {
		t.Fatal("bad whole hash accepted")
	}
}

func TestFetchCloneKitManifestBoundary(t *testing.T) {
	m := validCloneKitManifest()
	body, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, body string
		status     int
		valid      bool
	}{
		{"valid", string(body), 200, true},
		{"additive", strings.TrimSuffix(string(body), "}") + `,"future":{"values":[1,2]}}`, 200, true},
		{"duplicate", strings.TrimSuffix(string(body), "}") + `,"version":1}`, 200, false},
		{"case duplicate", strings.TrimSuffix(string(body), "}") + `,"VERSION":1}`, 200, false},
		{"malformed", `{`, 200, false},
		{"oversize", strings.Repeat(" ", (1<<20)+1), 200, false},
		{"miss", "", 404, false},
		{"unauthorized", "repository-secret", 401, false},
		{"redirect", "", 302, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				user, pass, ok := r.BasicAuth()
				if !ok || user != "user" || pass != "repository-secret" {
					t.Error("manifest credentials missing")
				}
				if tc.status == 302 {
					w.Header().Set("Location", "https://forbidden.example/secret")
				}
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer s.Close()
			client := s.Client()
			client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
			_, err := fetchCloneKitManifest(t.Context(), client, s.URL+"?secret=presigned-secret", "user", "repository-secret")
			if (err == nil) != tc.valid {
				t.Fatalf("error=%v valid=%v", err, tc.valid)
			}
			if err != nil && (strings.Contains(err.Error(), "repository-secret") || strings.Contains(err.Error(), "presigned-secret")) {
				t.Fatal("secret in error")
			}
		})
	}
}
