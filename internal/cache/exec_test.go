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
	"github.com/buildkite/agent/v4/jobapi"
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
	// Section headers go to stderr, so stdout is only the command's output.
	wantStderr := "--- :package: Restoring cache...\n+++ :package: No cached result, running command\nwarning: deprecated\n--- :package: Saving cache...\n"
	if stdout != "compiling\ndone\n" || stderr != wantStderr {
		t.Errorf("miss output: stdout %q, stderr %q", stdout, stderr)
	}
	if got := len(reg.entries); got != 2 {
		t.Fatalf("registry has %d entries after a miss, want main entry and output sidecar", got)
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
	// The replay header's title depends on timing, so check around it.
	if stdout != "compiling\ndone\n" || !strings.HasPrefix(stderr, "--- :package: Restoring cache...\n+++ ") ||
		!strings.Contains(stderr, "\nwarning: deprecated\n") || !strings.Contains(stderr, "Restored from cache") {
		t.Errorf("hit output: stdout %q, stderr %q", stdout, stderr)
	}

	for _, req := range reg.retrieves {
		for _, part := range req.CacheKey {
			if !part.Mandatory {
				t.Errorf("retrieve %v has an optional key part; exec must only match exactly", req.TargetPaths)
			}
		}
	}

	// Plain restore of the same cache gets only the target paths.
	if err := os.RemoveAll("out"); err != nil {
		t.Fatal(err)
	}
	if err := RunRestore(t.Context(), logger.Discard, apiClient, cfg); err != nil {
		t.Fatalf("plain restore: %v", err)
	}
	if got := readFile(t, filepath.Join("out", "result")); got != "built" {
		t.Errorf("plain restore: out/result = %q, want built", got)
	}
	if entries, _ := os.ReadDir("."); len(entries) != 3 { // cache.yml, input.txt, out
		t.Errorf("plain restore left unexpected files: %v", entries)
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
	if !strings.HasSuffix(stdout, "failing\n") {
		t.Errorf("stdout = %q, want the command's output", stdout)
	}
	if len(reg.stores) != 0 {
		t.Errorf("failed command saved %d entries, want none", len(reg.stores))
	}
}

func TestRunExec_MainEntryFromPlainSaveIsReplaced(t *testing.T) {
	reg, apiClient, cfg := setupExecTest(t)
	writeFile(t, filepath.Join("out", "result"), "incomplete, from cache save")
	if err := RunSave(t.Context(), logger.Discard, apiClient, cfg); err != nil {
		t.Fatalf("plain save: %v", err)
	}

	// Files without recorded output aren't a hit, so they're left alone and the command runs.
	b := &build{}
	writeFile(t, "input.txt", "v1")
	if _, _, err := runExec(t, apiClient, cfg, func(stdout, stderr io.Writer) error {
		if got := readFile(t, filepath.Join("out", "result")); got != "incomplete, from cache save" {
			t.Errorf("out/result = %q before the command ran, want target_paths left alone", got)
		}
		return b.run(stdout, stderr)
	}); err != nil {
		t.Fatalf("first exec: %v", err)
	}

	// The run saved its output and replaced the files with its own, so the next exec hits with both.
	writeFile(t, "input.txt", "v1")
	writeFile(t, filepath.Join("out", "result"), "stale")
	stdout, _, err := runExec(t, apiClient, cfg, b.run)
	if err != nil {
		t.Fatalf("second exec: %v", err)
	}
	if b.runs != 1 || stdout != "compiling\ndone\n" {
		t.Errorf("command ran %d times, stdout %q; want 1 run and the replayed output", b.runs, stdout)
	}
	if got := readFile(t, filepath.Join("out", "result")); got != "built" {
		t.Errorf("out/result = %q, want the files from the run that recorded the output", got)
	}
	if got := len(reg.stores); got != 3 {
		t.Errorf("got %d stores, want the plain save, the output and the replacement files", got)
	}
}

func TestRunExec_ReplacedMainEntryIsNotPairedWithOldOutput(t *testing.T) {
	_, apiClient, cfg := setupExecTest(t)
	b := &build{}
	if _, _, err := runExec(t, apiClient, cfg, b.run); err != nil {
		t.Fatalf("first exec: %v", err)
	}

	// Replace the main entry with different files, as cache save --force does.
	writeFile(t, "input.txt", "v1")
	writeFile(t, filepath.Join("out", "result"), "incomplete, from a forced save")
	forced := cfg
	forced.Force = true
	if err := RunSave(t.Context(), logger.Discard, apiClient, forced); err != nil {
		t.Fatalf("forced save: %v", err)
	}

	// The recorded output belongs to the replaced files, so this misses and leaves target_paths alone.
	writeFile(t, filepath.Join("out", "local"), "untouched")
	stdout, _, err := runExec(t, apiClient, cfg, func(stdout, stderr io.Writer) error {
		if got := readFile(t, filepath.Join("out", "local")); got != "untouched" {
			t.Errorf("out/local = %q before the command ran, want target_paths left alone", got)
		}
		return b.run(stdout, stderr)
	})
	if err != nil {
		t.Fatalf("second exec: %v", err)
	}
	if b.runs != 2 {
		t.Errorf("command ran %d times, want 2: old output was replayed with replaced files", b.runs)
	}
	if strings.Contains(stdout, "Replaying output from cache") {
		t.Errorf("stdout = %q, want no replay", stdout)
	}
}

func TestRunExec_RedactsSavedOutput(t *testing.T) {
	_, apiClient, cfg := setupExecTest(t)
	// The job learns the secret while the command runs, as with secret get.
	var registered []string
	cfg.Redact = func(ctx context.Context, chunks []jobapi.OutputChunk) ([]jobapi.OutputChunk, error) {
		return redactWith(registered...)(ctx, chunks)
	}
	token := "bkua_" + strings.Repeat("a1B2", 10)
	printSecrets := func(stdout, stderr io.Writer) error {
		registered = append(registered, "hunter2-secret")
		// Split the secret across writes, as a command's output may be.
		_, _ = io.WriteString(stdout, "password: hunter2-")
		_, _ = io.WriteString(stdout, "secret\n")
		_, _ = io.WriteString(stderr, "token: "+token+"\n")
		return os.MkdirAll("out", 0o755)
	}
	if _, _, err := runExec(t, apiClient, cfg, printSecrets); err != nil {
		t.Fatalf("first exec: %v", err)
	}

	// Replay in a job that has no secrets to redact.
	cfg.Redact = nil
	stdout, stderr, err := runExec(t, apiClient, cfg, func(io.Writer, io.Writer) error {
		t.Error("command ran on a cache hit")
		return nil
	})
	if err != nil {
		t.Fatalf("second exec: %v", err)
	}
	if stdout != "password: [REDACTED]\n" || !strings.Contains(stderr, "\ntoken: [REDACTED]\n") {
		t.Errorf("replayed output not redacted: stdout %q, stderr %q", stdout, stderr)
	}
}

// redactWith redacts needles and Buildkite tokens, as the Job API does.
func redactWith(needles ...string) Redactor {
	return func(_ context.Context, chunks []jobapi.OutputChunk) ([]jobapi.OutputChunk, error) {
		return jobapi.RedactChunks(chunks, needles, redact.TokenPrefixes()...)
	}
}

func TestRunExec_RedactionFails(t *testing.T) {
	for _, test := range []struct {
		name        string
		failOnError bool
	}{
		{name: "saves nothing by default"},
		{name: "fails with cache-fail-on-error", failOnError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			reg, apiClient, cfg := setupExecTest(t)
			cfg.FailOnError = test.failOnError
			cfg.Redact = func(context.Context, []jobapi.OutputChunk) ([]jobapi.OutputChunk, error) {
				return nil, errors.New("job API went away")
			}
			b := &build{}
			_, _, err := runExec(t, apiClient, cfg, b.run)
			if gotErr := err != nil; gotErr != test.failOnError {
				t.Errorf("exec error = %v, want error: %t", err, test.failOnError)
			}
			if b.runs != 1 {
				t.Errorf("command ran %d times, want 1", b.runs)
			}
			if len(reg.stores) != 0 {
				t.Errorf("saved %d entries without redacting the output, want none", len(reg.stores))
			}
		})
	}
}

func TestRunExec_HeaderStartsOnNewLine(t *testing.T) {
	_, apiClient, cfg := setupExecTest(t)
	stdout, stderr, err := runExec(t, apiClient, cfg, func(stdout, _ io.Writer) error {
		_, _ = io.WriteString(stdout, "no trailing newline")
		return os.MkdirAll("out", 0o755)
	})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if stdout != "no trailing newline" {
		t.Errorf("stdout = %q, want only the command's output", stdout)
	}
	if !strings.Contains(stderr, "running command\n\n--- :package: Saving cache...\n") {
		t.Errorf("stderr = %q, want the Saving header to start on a new line", stderr)
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

func TestOutputRecorderRoundTrip(t *testing.T) {
	rec, err := newOutputRecorder()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(rec.Path()) })
	_, _ = io.WriteString(rec.Stdout(), "one\n")
	_, _ = io.WriteString(rec.Stderr(), "two\n")
	_, _ = io.WriteString(rec.Stdout(), "")
	_, _ = io.WriteString(rec.Stdout(), "three\n")
	info, err := rec.Close(1234 * time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if info.WrittenBytes != 14 {
		t.Errorf("WrittenBytes = %d, want 14", info.WrittenBytes)
	}

	var combined, stderr bytes.Buffer
	ranFor, err := replayOutput(info.ArchivePath, &combined, io.MultiWriter(&combined, &stderr))
	if err != nil {
		t.Fatal(err)
	}
	if ranFor != 1234*time.Millisecond {
		t.Errorf("replayed run time = %v, want 1.234s", ranFor)
	}
	if got, want := combined.String(), "one\ntwo\nthree\n"; got != want {
		t.Errorf("replayed output = %q, want %q in the original order", got, want)
	}
	if got := stderr.String(); got != "two\n" {
		t.Errorf("replayed stderr = %q, want two", got)
	}

	if err := os.WriteFile(info.ArchivePath, []byte("not a recording"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := replayOutput(info.ArchivePath, io.Discard, io.Discard); err == nil {
		t.Error("replaying a corrupt recording should fail")
	}
}

func TestRunExec_LargeOutputIsNotSaved(t *testing.T) {
	reg, apiClient, cfg := setupExecTest(t)
	defer func(n int64) { maxRecordedOutput = n }(maxRecordedOutput)
	maxRecordedOutput = 10

	stdout, _, err := runExec(t, apiClient, cfg, func(stdout, _ io.Writer) error {
		_, _ = io.WriteString(stdout, "more than ten bytes\n")
		return os.MkdirAll("out", 0o755)
	})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if stdout != "more than ten bytes\n" {
		t.Errorf("stdout = %q, want all the output live", stdout)
	}
	if len(reg.stores) != 0 {
		t.Errorf("saved %d entries for output over the limit, want none", len(reg.stores))
	}
}

func TestTimeSaved(t *testing.T) {
	for _, test := range []struct {
		ranFor, took time.Duration
		want         string
	}{
		{ranFor: 105 * time.Second, took: 42 * time.Millisecond, want: "\x1b[32m✔\x1b[0m Restored from cache"},
		{took: 2 * time.Second, want: "\x1b[32m✔\x1b[0m Restored from cache"},
		{ranFor: time.Second, took: 3 * time.Second, want: "\x1b[33m⚠\x1b[0m Restored from cache in \x1b[1m3s\x1b[0m, but running the command took only \x1b[1m1s\x1b[0m: caching it isn't saving time"},
	} {
		if got := timeSaved(test.ranFor, test.took); got != test.want {
			t.Errorf("timeSaved(%v, %v) = %q, want %q", test.ranFor, test.took, got, test.want)
		}
	}

	if got, want := replayHeader(103*time.Second), "+++ ⚡ \x1b[1;32mcache exec saved 1m43s\x1b[0m"; got != want {
		t.Errorf("replayHeader(1m43s) = %q, want %q", got, want)
	}
	if got := replayHeader(-time.Second); !strings.HasPrefix(got, "+++ :package: Replaying output from cache") {
		t.Errorf("replayHeader with nothing saved = %q, want the plain header", got)
	}
}
