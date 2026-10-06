package cache

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/internal/redact"
	"github.com/buildkite/agent/v4/logger"
)

// fakeRegistry is an in-memory registry served over HTTP that matches exact addresses only, as the backend does for all-mandatory keys.
type fakeRegistry struct {
	mu        sync.Mutex
	entries   map[string]api.CacheEntryRetrieveResp
	pending   map[string]api.CacheEntryRetrieveResp
	stores    []api.CacheEntryCreateReq
	retrieves []api.CacheEntryRetrieveReq
}

func fakeAddr(targetPaths []string, key []api.CacheKeyPart) string {
	paths := slices.Sorted(slices.Values(targetPaths))
	var values []string
	for _, p := range key {
		values = append(values, p.Value)
	}
	return strings.Join(paths, "\x00") + "#" + strings.Join(values, "#")
}

func (f *fakeRegistry) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	notFound := func() {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"message":"Cache entry not found"}`)
	}
	reply := func(v any) { _ = json.NewEncoder(w).Encode(v) }

	switch r.Method + " " + r.URL.Path {
	case "GET /cache_registries/test":
		reply(api.CacheRegistryResp{Store: "local_file"})
	case "POST /cache_registries/test/peek":
		var req api.CacheEntryPeekReq
		_ = json.NewDecoder(r.Body).Decode(&req)
		entry, ok := f.entries[fakeAddr(req.TargetPaths, req.CacheKey)]
		if !ok {
			notFound()
			return
		}
		reply(api.CacheEntryPeekResp{Blobs: entry.Blobs})
	case "POST /cache_registries/test/retrieve":
		var req api.CacheEntryRetrieveReq
		_ = json.NewDecoder(r.Body).Decode(&req)
		f.retrieves = append(f.retrieves, req)
		entry, ok := f.entries[fakeAddr(req.TargetPaths, req.CacheKey)]
		if !ok {
			notFound()
			return
		}
		reply(entry)
	case "PUT /cache_registries/test/store":
		var req api.CacheEntryCreateReq
		_ = json.NewDecoder(r.Body).Decode(&req)
		f.stores = append(f.stores, req)
		id := fmt.Sprintf("upload-%d", len(f.stores))
		f.pending[id] = api.CacheEntryRetrieveResp{TargetPaths: req.TargetPaths, CacheKey: req.CacheKey, Blobs: req.Blobs, Store: "local_file"}
		reply(api.CacheEntryCreateResp{UploadID: id})
	case "PUT /cache_registries/test/commit":
		var req api.CacheEntryCommitReq
		_ = json.NewDecoder(r.Body).Decode(&req)
		entry := f.pending[req.UploadID]
		f.entries[fakeAddr(entry.TargetPaths, entry.CacheKey)] = entry
		reply(api.CacheEntryCommitResp{})
	case "POST /cache_registries/test/expire":
		var req api.CacheEntryExpireReq
		_ = json.NewDecoder(r.Body).Decode(&req)
		addr := fakeAddr(req.TargetPaths, req.CacheKey)
		_, existed := f.entries[addr]
		delete(f.entries, addr)
		reply(api.CacheEntryExpireResp{Existed: existed})
	case "POST /cache_registries/test/confirm":
		reply(api.CacheEntryConfirmResp{})
	default:
		w.WriteHeader(http.StatusBadRequest)
	}
}

// setupExecTest creates a workspace whose cache key allows a fallback on input.txt, and a config for a fake registry.
func setupExecTest(t *testing.T) (*fakeRegistry, *api.Client, Config) {
	t.Helper()
	t.Chdir(t.TempDir())
	writeFile(t, "cache.yml", `caches:
  - name: build
    cache_key:
      - { agent: os, fallback_limit: true }
      - { checksum: input.txt }
    target_paths: [out]
`)
	writeFile(t, "input.txt", "v1")

	reg := &fakeRegistry{entries: map[string]api.CacheEntryRetrieveResp{}, pending: map[string]api.CacheEntryRetrieveResp{}}
	server := httptest.NewServer(reg)
	t.Cleanup(server.Close)

	storage := filepath.ToSlash(t.TempDir())
	if !strings.HasPrefix(storage, "/") {
		storage = "/" + storage
	}
	cfg := Config{
		Registry:        "test",
		BucketURL:       (&url.URL{Scheme: "file", Path: storage}).String(),
		CacheConfigFile: "cache.yml",
		Names:           []string{"build"},
		Redact:          redactWith(),
	}
	return reg, api.NewClient(logger.Discard, api.Config{Endpoint: server.URL, Token: "token"}), cfg
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// build counts its runs, writes out/result, prints to both streams, then changes a cache key input.
type build struct{ runs int }

func (b *build) run(stdout, stderr io.Writer) error {
	b.runs++
	if err := os.MkdirAll("out", 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join("out", "result"), []byte("built"), 0o600); err != nil {
		return err
	}
	_, _ = io.WriteString(stdout, "compiling\n")
	_, _ = io.WriteString(stderr, "warning: deprecated\n")
	_, _ = io.WriteString(stdout, "done\n")
	return os.WriteFile("input.txt", []byte("changed by the build"), 0o600)
}

func runExec(t *testing.T, apiClient *api.Client, cfg Config, cmd Command) (stdout, stderr string, err error) {
	t.Helper()
	var o, e bytes.Buffer
	err = RunExec(t.Context(), logger.Discard, apiClient, cfg, &o, &e, cmd)
	return o.String(), e.String(), err
}

func TestRunExec_MissThenHit(t *testing.T) {
	reg, apiClient, cfg := setupExecTest(t)
	b := &build{}

	stdout, stderr, err := runExec(t, apiClient, cfg, b.run)
	if err != nil {
		t.Fatalf("first exec: %v", err)
	}
	if b.runs != 1 {
		t.Fatalf("command ran %d times on a miss, want 1", b.runs)
	}
	// Headers go to stderr, so stdout is only the command's output.
	wantStderr := "--- :package: Restoring cache\n+++ :package: No cached result, running command\nwarning: deprecated\n--- :package: Saving cache\n"
	if stdout != "compiling\ndone\n" || stderr != wantStderr {
		t.Errorf("miss output: stdout %q, stderr %q", stdout, stderr)
	}
	if got := len(reg.stores); got != 1 {
		t.Fatalf("got %d stores after a miss, want one entry with the files and the log", got)
	}
	if got, want := reg.stores[0].TargetPaths, []string{"out", ".buildkite-cache-exec-build.log"}; !slices.Equal(got, want) {
		t.Errorf("saved entry target_paths = %q, want %q", got, want)
	}

	// Simulate a fresh checkout: original key inputs, stale out contents.
	writeFile(t, "input.txt", "v1")
	if err := os.RemoveAll("out"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join("out", "stale"), "old")

	stdout, stderr, err = runExec(t, apiClient, cfg, b.run)
	if err != nil {
		t.Fatalf("second exec: %v", err)
	}
	if b.runs != 1 {
		t.Errorf("command ran on a cache hit")
	}
	if got := readFile(t, filepath.Join("out", "result")); got != "built" {
		t.Errorf("out/result = %q, want restored contents", got)
	}
	if _, err := os.Stat(filepath.Join("out", "stale")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("restore should replace out, but out/stale remains (err %v)", err)
	}
	if stdout != "compiling\nwarning: deprecated\ndone\n" || !strings.Contains(stderr, "the command was not run") {
		t.Errorf("hit output: stdout %q, stderr %q", stdout, stderr)
	}

	for _, req := range reg.retrieves {
		for _, part := range req.CacheKey {
			if !part.Mandatory {
				t.Errorf("retrieve %v has an optional key part; exec must only match exactly", req.TargetPaths)
			}
		}
	}
	if entries, _ := os.ReadDir("."); len(entries) != 3 { // cache.yml, input.txt, out
		t.Errorf("exec left unexpected files in the workspace: %v", entries)
	}
	if got := len(reg.stores); got != 1 {
		t.Errorf("got %d stores after a hit, want none added", got-1)
	}
}

func TestRunExec_FailedCommandSavesNothing(t *testing.T) {
	reg, apiClient, cfg := setupExecTest(t)
	boom := errors.New("exit status 2")

	stdout, _, err := runExec(t, apiClient, cfg, func(stdout, _ io.Writer) error {
		_, _ = io.WriteString(stdout, "failing\n")
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("exec error = %v, want the command's error", err)
	}
	if stdout != "failing\n" {
		t.Errorf("stdout = %q, want the command's output", stdout)
	}
	if len(reg.stores) != 0 {
		t.Errorf("failed command saved %d entries, want none", len(reg.stores))
	}
}

func TestRunExec_SeparateFromPlainSave(t *testing.T) {
	reg, apiClient, cfg := setupExecTest(t)
	writeFile(t, filepath.Join("out", "result"), "from cache save")
	if err := RunSave(t.Context(), logger.Discard, apiClient, cfg); err != nil {
		t.Fatalf("plain save: %v", err)
	}

	// A plain save's entry has no log, so it isn't a hit: target_paths are left alone and the command runs.
	b := &build{}
	if _, _, err := runExec(t, apiClient, cfg, func(stdout, stderr io.Writer) error {
		if got := readFile(t, filepath.Join("out", "result")); got != "from cache save" {
			t.Errorf("out/result = %q before the command ran, want target_paths left alone", got)
		}
		return b.run(stdout, stderr)
	}); err != nil {
		t.Fatalf("exec: %v", err)
	}
	if got := len(reg.entries); got != 2 {
		t.Fatalf("registry has %d entries, want the plain save's and exec's", got)
	}

	// Plain restore still gets the plain save's files.
	writeFile(t, "input.txt", "v1")
	if err := RunRestore(t.Context(), logger.Discard, apiClient, cfg); err != nil {
		t.Fatalf("plain restore: %v", err)
	}
	if got := readFile(t, filepath.Join("out", "result")); got != "from cache save" {
		t.Errorf("plain restore: out/result = %q, want the plain save's files", got)
	}
}

// redactWith redacts needles and Buildkite tokens, as the Job API does.
func redactWith(needles ...string) Redactor {
	return func(_ context.Context, output []byte) ([]byte, error) {
		var out bytes.Buffer
		r := redact.New(&out, needles)
		r.AddPrefixes(redact.TokenPrefixes()...)
		_, _ = r.Write(output)
		return out.Bytes(), r.Flush()
	}
}

func TestRunExec_RedactsSavedOutput(t *testing.T) {
	_, apiClient, cfg := setupExecTest(t)
	// The job learns the secret while the command runs, as with secret get.
	var registered []string
	cfg.Redact = func(ctx context.Context, output []byte) ([]byte, error) {
		return redactWith(registered...)(ctx, output)
	}
	token := "bkua_" + strings.Repeat("a1B2", 10)
	if _, _, err := runExec(t, apiClient, cfg, func(stdout, stderr io.Writer) error {
		registered = append(registered, "hunter2-secret")
		_, _ = io.WriteString(stdout, "password: hunter2-")
		_, _ = io.WriteString(stdout, "secret\n")
		_, _ = io.WriteString(stderr, "token: "+token+"\n")
		return os.MkdirAll("out", 0o755)
	}); err != nil {
		t.Fatalf("first exec: %v", err)
	}

	cfg.Redact = nil // a hit doesn't redact again
	stdout, _, err := runExec(t, apiClient, cfg, func(io.Writer, io.Writer) error {
		t.Error("command ran on a cache hit")
		return nil
	})
	if err != nil {
		t.Fatalf("second exec: %v", err)
	}
	if want := "password: [REDACTED]\ntoken: [REDACTED]\n"; stdout != want {
		t.Errorf("replayed output = %q, want %q", stdout, want)
	}
}

func TestRunExec_OutputNotSaved(t *testing.T) {
	for _, test := range []struct {
		name        string
		redact      Redactor
		output      string
		failOnError bool
	}{
		{name: "redaction fails", redact: func(context.Context, []byte) ([]byte, error) { return nil, errors.New("job API went away") }},
		{name: "redaction fails with cache-fail-on-error", redact: func(context.Context, []byte) ([]byte, error) { return nil, errors.New("job API went away") }, failOnError: true},
		{name: "output too large", redact: redactWith(), output: strings.Repeat("x", maxOutput+1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			reg, apiClient, cfg := setupExecTest(t)
			cfg.FailOnError = test.failOnError
			cfg.Redact = test.redact
			_, _, err := runExec(t, apiClient, cfg, func(stdout, _ io.Writer) error {
				_, _ = io.WriteString(stdout, test.output)
				return os.MkdirAll("out", 0o755)
			})
			if gotErr := err != nil; gotErr != test.failOnError {
				t.Errorf("exec error = %v, want error: %t", err, test.failOnError)
			}
			if len(reg.stores) != 0 {
				t.Errorf("saved %d entries, want none", len(reg.stores))
			}
		})
	}
}

func TestRunExec_CacheKeyFailure(t *testing.T) {
	for _, test := range []struct {
		name        string
		failOnError bool
		wantRuns    int
	}{
		{name: "runs the command uncached by default", wantRuns: 1},
		{name: "fails with cache-fail-on-error", failOnError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			reg, apiClient, cfg := setupExecTest(t)
			cfg.FailOnError = test.failOnError
			if err := os.Remove("input.txt"); err != nil { // a checksum input
				t.Fatal(err)
			}
			b := &build{}
			_, _, err := runExec(t, apiClient, cfg, b.run)
			if gotErr := err != nil; gotErr != test.failOnError {
				t.Errorf("exec error = %v, want error: %t", err, test.failOnError)
			}
			if b.runs != test.wantRuns {
				t.Errorf("command ran %d times, want %d", b.runs, test.wantRuns)
			}
			if len(reg.retrieves) != 0 || len(reg.stores) != 0 {
				t.Errorf("used the cache without a key: %d retrieves, %d stores", len(reg.retrieves), len(reg.stores))
			}
		})
	}
}

func TestReplayHeader(t *testing.T) {
	if got, want := replayHeader(103*time.Second), "+++ ⚡ \x1b[1;32mCache hit saved 1m43s\x1b[0m: replaying output, the command was not run"; got != want {
		t.Errorf("replayHeader(1m43s) = %q, want %q", got, want)
	}
	if got, want := replayHeader(-time.Second), "+++ :package: Cache hit: replaying output, the command was not run"; got != want {
		t.Errorf("replayHeader(-1s) = %q, want %q", got, want)
	}
}
