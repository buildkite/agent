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
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/logger"
)

// fakeRegistry is an in-memory registry served over HTTP that matches exact addresses only, as the backend does for all-mandatory keys.
type fakeRegistry struct {
	mu        sync.Mutex
	entries   map[string]api.CacheEntryRetrieveResp
	pending   map[string]api.CacheEntryRetrieveResp
	stores    []api.CacheEntryCreateReq
	retrieves []api.CacheEntryRetrieveReq
	confirms  []api.CacheEntryConfirmReq
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
		var req api.CacheEntryConfirmReq
		_ = json.NewDecoder(r.Body).Decode(&req)
		f.confirms = append(f.confirms, req)
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
	wantStderr = "--- :package: Restoring cache...\n+++ :package: Replaying output from cache (command was not run)\nwarning: deprecated\n"
	if stdout != "compiling\ndone\n" || stderr != wantStderr {
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

func TestRunExec_MainEntryFromPlainSaveIsNeverReplayed(t *testing.T) {
	reg, apiClient, cfg := setupExecTest(t)
	writeFile(t, filepath.Join("out", "result"), "incomplete, from cache save")
	if err := RunSave(t.Context(), logger.Discard, apiClient, cfg); err != nil {
		t.Fatalf("plain save: %v", err)
	}

	// No recorded output means no hit, and output must not be saved next to files this run didn't produce.
	b := &build{}
	for run := 1; run <= 2; run++ {
		writeFile(t, "input.txt", "v1")
		if _, _, err := runExec(t, apiClient, cfg, b.run); err != nil {
			t.Fatalf("exec %d: %v", run, err)
		}
		if b.runs != run {
			t.Fatalf("after exec %d the command ran %d times, want %d", run, b.runs, run)
		}
	}
	if got := len(reg.stores); got != 1 {
		t.Errorf("got %d stores, want only the plain save", got)
	}
}

func TestRunExec_MainEntryFromPlainSaveOfIdenticalFilesGetsOutput(t *testing.T) {
	reg, apiClient, cfg := setupExecTest(t)
	writeFile(t, filepath.Join("out", "result"), "built") // what build.run writes
	if err := RunSave(t.Context(), logger.Discard, apiClient, cfg); err != nil {
		t.Fatalf("plain save: %v", err)
	}

	// The first exec has no output to replay, but it produces the saved files, so it records output for them.
	b := &build{}
	writeFile(t, "input.txt", "v1")
	if _, _, err := runExec(t, apiClient, cfg, b.run); err != nil {
		t.Fatalf("first exec: %v", err)
	}
	if got := len(reg.stores); got != 2 {
		t.Fatalf("got %d stores, want the plain save and the output sidecar", got)
	}

	writeFile(t, "input.txt", "v1")
	stdout, _, err := runExec(t, apiClient, cfg, b.run)
	if err != nil {
		t.Fatalf("second exec: %v", err)
	}
	if b.runs != 1 {
		t.Errorf("command ran %d times, want 1: the second exec should hit", b.runs)
	}
	if stdout != "compiling\ndone\n" {
		t.Errorf("stdout = %q, want replayed output", stdout)
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
	cfg.Redactions = staticRedactions("hunter2-secret")
	token := "bkua_" + strings.Repeat("a1B2", 10)
	printSecrets := func(stdout, stderr io.Writer) error {
		if err := os.MkdirAll("out", 0o755); err != nil {
			return err
		}
		// Split the secret across writes, as a command's output may be.
		_, _ = io.WriteString(stdout, "password: hunter2-")
		_, _ = io.WriteString(stdout, "secret\n")
		_, _ = io.WriteString(stderr, "token: "+token+"\n")
		return nil
	}
	if _, _, err := runExec(t, apiClient, cfg, printSecrets); err != nil {
		t.Fatalf("first exec: %v", err)
	}

	// Replay in a job that has no secrets to redact.
	cfg.Redactions = nil
	stdout, stderr, err := runExec(t, apiClient, cfg, func(io.Writer, io.Writer) error {
		t.Error("command ran on a cache hit")
		return nil
	})
	if err != nil {
		t.Fatalf("second exec: %v", err)
	}
	if stdout != "password: [REDACTED]\n" || !strings.HasSuffix(stderr, "\ntoken: [REDACTED]\n") {
		t.Errorf("replayed output not redacted: stdout %q, stderr %q", stdout, stderr)
	}
}

func staticRedactions(needles ...string) func(context.Context) ([]string, error) {
	return func(context.Context) ([]string, error) { return needles, nil }
}

func TestRunExec_RedactsSecretsRegisteredByTheCommand(t *testing.T) {
	_, apiClient, cfg := setupExecTest(t)
	// The job only learns the secret while the command runs, as with secret get.
	var registered []string
	cfg.Redactions = func(context.Context) ([]string, error) { return slices.Clone(registered), nil }
	if _, _, err := runExec(t, apiClient, cfg, func(stdout, _ io.Writer) error {
		registered = append(registered, "late-secret-value")
		_, _ = io.WriteString(stdout, "fetched late-secret-value\n")
		return os.MkdirAll("out", 0o755)
	}); err != nil {
		t.Fatalf("first exec: %v", err)
	}

	cfg.Redactions = nil
	stdout, _, err := runExec(t, apiClient, cfg, func(io.Writer, io.Writer) error {
		t.Error("command ran on a cache hit")
		return nil
	})
	if err != nil {
		t.Fatalf("second exec: %v", err)
	}
	if !strings.HasSuffix(stdout, "fetched [REDACTED]\n") {
		t.Errorf("replayed stdout = %q, want the late secret redacted", stdout)
	}
}

func TestRunExec_RedactionsUnavailableAfterCommand(t *testing.T) {
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
			calls := 0
			cfg.Redactions = func(context.Context) ([]string, error) {
				if calls++; calls > 1 {
					return nil, errors.New("job API went away")
				}
				return nil, nil
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
				t.Errorf("saved %d entries without knowing every secret, want none", len(reg.stores))
			}
		})
	}
}

func TestRunExec_RedactionsUnavailableBeforeCommand(t *testing.T) {
	reg, apiClient, cfg := setupExecTest(t)
	b := &build{}
	if _, _, err := runExec(t, apiClient, cfg, b.run); err != nil {
		t.Fatalf("first exec: %v", err)
	}

	// Without the secrets, a saved result can still be replayed: it was redacted when it was saved.
	cfg.Redactions = func(context.Context) ([]string, error) { return nil, errors.New("no Job API") }
	writeFile(t, "input.txt", "v1")
	stdout, _, err := runExec(t, apiClient, cfg, b.run)
	if err != nil {
		t.Fatalf("exec with a saved result: %v", err)
	}
	if b.runs != 1 || stdout != "compiling\ndone\n" {
		t.Errorf("command ran %d times, stdout %q; want 1 run and the replayed output", b.runs, stdout)
	}

	// On a miss the command runs, but its result isn't saved.
	writeFile(t, "input.txt", "v2")
	stores := len(reg.stores)
	if _, _, err := runExec(t, apiClient, cfg, b.run); err != nil {
		t.Fatalf("exec on a miss: %v", err)
	}
	if b.runs != 2 {
		t.Errorf("command ran %d times, want 2", b.runs)
	}
	if got := len(reg.stores) - stores; got != 0 {
		t.Errorf("saved %d entries without knowing every secret, want none", got)
	}

	cfg.FailOnError = true
	if _, _, err := runExec(t, apiClient, cfg, b.run); err == nil {
		t.Error("exec succeeded without the secrets, want an error with cache-fail-on-error")
	}
}

func TestRunExec_UnknownCacheName(t *testing.T) {
	_, apiClient, cfg := setupExecTest(t)
	cfg.Names = []string{"unknown"}
	b := &build{}
	if _, _, err := runExec(t, apiClient, cfg, b.run); err != nil {
		t.Fatalf("exec: %v", err)
	}
	if b.runs != 1 {
		t.Errorf("command ran %d times, want 1: an unknown name runs it uncached", b.runs)
	}
	cfg.FailOnError = true
	if _, _, err := runExec(t, apiClient, cfg, b.run); err == nil {
		t.Error("exec succeeded with an unknown name, want an error with cache-fail-on-error")
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

func TestRunExec_FailedRestoreDoesNotConfirmOutput(t *testing.T) {
	reg, apiClient, cfg := setupExecTest(t)
	b := &build{}
	if _, _, err := runExec(t, apiClient, cfg, b.run); err != nil {
		t.Fatalf("first exec: %v", err)
	}

	// Lose the main entry's blob, so restoring it fails after the output was found.
	for _, entry := range reg.entries {
		if !slices.Contains(entry.TargetPaths, outputTargetPath) {
			storage, _ := url.Parse(cfg.BucketURL)
			dir := storage.Path
			if runtime.GOOS == "windows" {
				dir = strings.TrimPrefix(dir, "/") // "/C:/..." from setupExecTest
			}
			if err := os.Remove(filepath.Join(filepath.FromSlash(dir), entry.Blobs[0].Digest.Value)); err != nil {
				t.Fatal(err)
			}
		}
	}
	writeFile(t, "input.txt", "v1")
	if _, _, err := runExec(t, apiClient, cfg, b.run); err != nil {
		t.Fatalf("second exec: %v", err)
	}
	if b.runs != 2 {
		t.Errorf("command ran %d times, want 2", b.runs)
	}
	for _, req := range reg.confirms {
		if slices.Contains(req.TargetPaths, outputTargetPath) {
			t.Errorf("confirmed the output entry, but its files weren't restored")
		}
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
	rec, err := newOutputRecorder(nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(rec.Path()) })
	_, _ = io.WriteString(rec.Stdout(), "one\n")
	_, _ = io.WriteString(rec.Stderr(), "two\n")
	_, _ = io.WriteString(rec.Stdout(), "")
	_, _ = io.WriteString(rec.Stdout(), "three\n")
	info, err := rec.Close()
	if err != nil {
		t.Fatal(err)
	}
	if info.WrittenBytes != 14 {
		t.Errorf("WrittenBytes = %d, want 14", info.WrittenBytes)
	}

	var combined, stderr bytes.Buffer
	if err := replayOutput(info.ArchivePath, &combined, io.MultiWriter(&combined, &stderr)); err != nil {
		t.Fatal(err)
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
	if err := replayOutput(info.ArchivePath, io.Discard, io.Discard); err == nil {
		t.Error("replaying a corrupt recording should fail")
	}
}
