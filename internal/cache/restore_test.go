package cache

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/internal/cache/configuration"
	"github.com/buildkite/agent/v4/internal/cache/store"
	"github.com/buildkite/agent/v4/logger"
)

func TestDownloadCacheLocalArchiveAndCleanup(t *testing.T) {
	content := []byte("cache archive contents")
	sum := sha256.Sum256(content)
	for _, outcome := range []string{"success", "unknown algorithm", "missing blob", "digest mismatch"} {
		t.Run(outcome, func(t *testing.T) {
			digest := api.CacheDigest{Algorithm: "sha256", Value: hex.EncodeToString(sum[:])}
			if outcome == "unknown algorithm" {
				// Preserve support for future digest algorithms and keep the remote
				// object name independent of the local archive filename.
				digest = api.CacheDigest{Algorithm: "blake3", Value: "nested/blob"}
			}
			storageRoot := t.TempDir()
			tempRoot := t.TempDir()
			for _, name := range []string{"TMPDIR", "TMP", "TEMP"} {
				t.Setenv(name, tempRoot)
			}
			if outcome != "missing blob" {
				stored := content
				if outcome == "digest mismatch" {
					stored = []byte("different contents")
				}
				storePath := filepath.Join(storageRoot, digest.Value)
				if err := os.MkdirAll(filepath.Dir(storePath), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(storePath, stored, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			bucketURL := (&url.URL{Scheme: "file", Path: "/" + strings.TrimPrefix(filepath.ToSlash(storageRoot), "/")}).String()
			c := &client{}
			tmpDir, archiveFile, _, err := c.downloadCache(t.Context(), api.CacheEntryRetrieveResp{
				Store: store.AgentManaged,
				Blobs: []api.CacheBlob{{Digest: digest}},
			}, bucketURL)
			switch outcome {
			case "success", "unknown algorithm":
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })
				if archiveFile != filepath.Join(tmpDir, "archive") {
					t.Fatalf("archiveFile = %q, want agent-chosen filename inside %q", archiveFile, tmpDir)
				}
				got, err := os.ReadFile(archiveFile)
				if err != nil || !bytes.Equal(got, content) {
					t.Fatalf("downloaded contents = %q, %v, want %q", got, err, content)
				}
				return
			case "missing blob":
				if !errors.Is(err, store.ErrBlobNotFound) {
					t.Fatalf("error = %v, want ErrBlobNotFound", err)
				}
			case "digest mismatch":
				if !errors.Is(err, ErrDigestMismatch) {
					t.Fatalf("error = %v, want ErrDigestMismatch", err)
				}
			}
			entries, err := os.ReadDir(tempRoot)
			if err != nil || len(entries) != 0 {
				t.Fatalf("failed download left temporary files: %v, %v", entries, err)
			}
		})
	}
}

func TestRestoreCleanupError(t *testing.T) {
	for _, tc := range []struct {
		name           string
		cause          error
		wantWriterHint bool
	}{
		{"non-empty directory", syscall.ENOTEMPTY, true},
		{"permission denied", os.ErrPermission, false},
		{"canceled", context.Canceled, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pathErr := &os.PathError{Op: "unlinkat", Path: "/shared/go-cache/76", Err: tc.cause}
			// Match cleanPath's wrapping; detection must use errors.Is rather
			// than a direct errno assertion or platform-dependent error text.
			err := restoreCleanupError("~/go-cache", "/shared/go-cache", fmt.Errorf("cleanPath: %w", pathErr))
			for _, text := range []string{"~/go-cache", "/shared/go-cache", pathErr.Error(), "extraction was not started", "may already have been modified"} {
				if !strings.Contains(err.Error(), text) {
					t.Errorf("error %q should contain %q", err, text)
				}
			}
			for _, text := range []string{"possibly because another process", "job-private target", "exclusive access for the entire time"} {
				if got := strings.Contains(err.Error(), text); got != tc.wantWriterHint {
					t.Errorf("error contains %q = %v, want %v: %v", text, got, tc.wantWriterHint, err)
				}
			}
			var gotPathErr *os.PathError
			if !errors.Is(err, tc.cause) || !errors.As(err, &gotPathErr) || gotPathErr != pathErr {
				t.Fatalf("filesystem cause was lost: %v", err)
			}
			mock := &mockCacheClient{restoreFunc: func(context.Context, string) (RestoreResult, error) {
				return RestoreResult{}, err
			}}
			if got := restoreWithClient(t.Context(), logger.Discard, mock, []string{"cache"}, 1, false); !errors.Is(got, errRestoreMutatedTargets) {
				t.Fatalf("cleanup failure must stay fatal when fail-open: %v", got)
			}
		})
	}
}

func TestRunRestore_Diagnostics(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.Mkdir("cache", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("cache/file", []byte("cached contents"), 0o600); err != nil {
		t.Fatal(err)
	}
	storageURL := (&url.URL{Scheme: "file", Path: "/" + strings.TrimPrefix(filepath.ToSlash(t.TempDir()), "/")}).String()
	mock := newMockAPIClient("local_file")
	c := &client{api: mock, registry: "~", bucketURL: storageURL, caches: []configuration.Cache{{
		Name: "npm", CacheKey: []configuration.KeyPart{{Source: configuration.SourceLiteral, Arg: "stored"}}, TargetPaths: []string{"cache"},
	}}}
	if _, err := c.Save(t.Context(), "npm"); err != nil {
		t.Fatal(err)
	}
	entry, _, _, err := mock.CacheEntryRetrieve(t.Context(), "~", api.CacheEntryRetrieveReq{
		TargetPaths: []string{"cache"}, CacheKey: []api.CacheKeyPart{{Value: "stored"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	configFile := createTempCacheConfig(t, "caches:\n  - name: npm\n    cache_key: [npm, v1, current]\n    target_paths: [cache]\n")
	for _, test := range []struct {
		name             string
		status           int
		attempts         string
		blobs            []api.CacheBlob
		want             string
		searchIncomplete bool
	}{
		{
			name:     "fallback hit",
			status:   http.StatusOK,
			attempts: `[{"cache_key":["npm","v1","current"],"scopes":{"pipeline":"ci"},"outcome":"miss"},{"cache_key":["npm","v1"],"scopes":{"pipeline":"ci"},"outcome":"hit"}]`,
			blobs:    entry.Blobs,
			want:     "Restoring cache: npm\n  npm-v1-current · pipeline=ci → miss\n  npm-v1         · pipeline=ci → hit\nCache restored using fallback key npm-v1-older from pipeline=ci, branch=main\n",
		},
		{
			name:     "policy denied",
			status:   http.StatusNotFound,
			attempts: `[{"cache_key":["npm","v1","current"],"scopes":{"pipeline":"ci"},"outcome":"denied","rule":"forks-read-only"}]`,
			want:     "Restoring cache: npm\n  npm-v1-current · pipeline=ci → denied (rule: forks-read-only)\nCache not restored: no allowed entry found (registry policy denied matching entries)\n",
		},
		{
			name:             "later candidates skipped",
			status:           http.StatusNotFound,
			attempts:         `[{"cache_key":["npm","v1","current"],"scopes":{},"outcome":"miss"}]`,
			searchIncomplete: true,
			want:             "Restoring cache: npm\n  npm-v1-current · any scope → miss\n  Registry search budget exhausted\nCache not restored: search incomplete\n",
		},
		{
			name:     "hit without downloadable blob",
			status:   http.StatusOK,
			attempts: `[{"cache_key":["npm","v1","current"],"scopes":{"pipeline":"ci"},"outcome":"hit"}]`,
			want:     "Restoring cache: npm\n  npm-v1-current · pipeline=ci → hit\nFailed to restore cache: failed to download cache: cache entry has no blobs to download; continuing without failing the build\n",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := os.WriteFile("cache/file", []byte("uncached contents"), 0o600); err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/cache_registries/~/retrieve" {
					t.Errorf("unexpected API path: %s", r.URL.Path)
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(test.status)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"message": "Cache entry not found", "store": "local_file", "fallback": true,
					"cache_key": []map[string]any{{"value": "npm"}, {"value": "v1"}, {"value": "older"}},
					"scopes":    map[string]string{"pipeline": "ci", "branch": "main"}, "blobs": test.blobs,
					"restore_diagnostics": map[string]any{
						"cache_key": []string{"npm", "v1", "current"}, "scope_candidates": []map[string]string{{"pipeline": "ci"}},
						"attempts": json.RawMessage(test.attempts), "budget_exhausted": test.searchIncomplete,
						"search_incomplete": test.searchIncomplete,
					},
				})
			}))
			defer server.Close()
			var output bytes.Buffer
			l := logger.NewConsoleLogger(logger.NewTextPrinter(&output), nil)
			l.SetLevel(logger.INFO)
			apiClient := api.NewClient(logger.Discard, api.Config{Endpoint: server.URL})
			if err := RunRestore(t.Context(), l, apiClient, Config{CacheConfigFile: configFile, BucketURL: storageURL}); err != nil {
				t.Fatal(err)
			}
			// Ignore only the timestamp/level prefix supplied by the console logger.
			_, got, ok := strings.Cut(output.String(), "Restoring cache:")
			if !ok || "Restoring cache:"+got != test.want || strings.Contains(output.String(), "Cache progress") {
				t.Errorf("output = %q, want report %q without progress chatter", output.String(), test.want)
			}
			wantContents := "uncached contents"
			if test.blobs != nil {
				wantContents = "cached contents"
			}
			contents, err := os.ReadFile("cache/file")
			if err != nil || string(contents) != wantContents {
				t.Fatalf("restored file = %q, want %q, err = %v", contents, wantContents, err)
			}
			t.Logf("\n%s", output.String())
		})
	}
}

func TestInvalidateStaleEntry_EchoesScopesFromRetrieve(t *testing.T) {
	mockClient := newMockAPIClient("s3")
	c := &client{api: mockClient, registry: "~"}

	scopes := map[string]string{"branch": "main"}
	targetPaths := []string{"node_modules"}
	cacheKey := []api.CacheKeyPart{{Value: "v1-test-key", Mandatory: true}}
	retrieveResp := api.CacheEntryRetrieveResp{
		TargetPaths: targetPaths,
		CacheKey:    cacheKey,
		Scopes:      scopes,
	}

	// An entry actually exists at this address, so expire should report a real
	// deletion, not an idempotent no-op.
	mockClient.registries["~"].cache[cacheAddr(targetPaths, cacheKey)] = &mockCacheEntry{
		targetPaths: targetPaths,
		cacheKey:    cacheKey,
		committed:   true,
	}

	invalidated := c.invalidateStaleEntry(t.Context(), retrieveResp)
	if !invalidated {
		t.Fatalf("invalidateStaleEntry() = false, want true")
	}

	if len(mockClient.expireCalls) != 1 {
		t.Fatalf("expire calls = %d, want 1", len(mockClient.expireCalls))
	}
	if got := mockClient.expireCalls[0].Scopes; !reflect.DeepEqual(got, scopes) {
		t.Errorf("expire request scopes = %+v, want %+v", got, scopes)
	}
}

// TestInvalidateStaleEntry_ExistedFalseIsNotReportedAsInvalidated guards against
// treating every successful (2xx) expire call as a deletion — expire is
// idempotent, so a 200 response can mean nothing was actually there to delete.
func TestInvalidateStaleEntry_ExistedFalseIsNotReportedAsInvalidated(t *testing.T) {
	mockClient := newMockAPIClient("s3")
	c := &client{api: mockClient, registry: "~"}

	// No entry exists at this address, so the mock's expire call succeeds but
	// reports existed: false.
	retrieveResp := api.CacheEntryRetrieveResp{
		TargetPaths: []string{"node_modules"},
		CacheKey:    []api.CacheKeyPart{{Value: "v1-test-key", Mandatory: true}},
	}

	invalidated := c.invalidateStaleEntry(t.Context(), retrieveResp)
	if invalidated {
		t.Error("invalidateStaleEntry() = true, want false for an idempotent no-op")
	}
}

// resavedEntry sets up the race where another job re-saved the address between
// this restore's retrieve and a later expire or confirm. The re-save has the
// same blob, as it usually will (deterministic archive of the same inputs), so
// only the upload ID distinguishes it.
func resavedEntry(mockClient *mockAPIClient) (api.CacheEntryRetrieveResp, *mockCacheEntry) {
	targetPaths := []string{"node_modules"}
	cacheKey := []api.CacheKeyPart{{Value: "v1-test-key", Mandatory: true}}
	blobs := []api.CacheBlob{{Digest: api.CacheDigest{Algorithm: "sha256", Value: "same"}}}

	retrieveResp := api.CacheEntryRetrieveResp{
		TargetPaths: targetPaths,
		CacheKey:    cacheKey,
		Blobs:       blobs,
		UploadID:    "stale-upload",
	}
	resaved := &mockCacheEntry{
		targetPaths: targetPaths,
		cacheKey:    cacheKey,
		blobs:       blobs,
		uploadID:    "fresh-upload",
		committed:   true,
	}
	mockClient.registries["~"].cache[cacheAddr(targetPaths, cacheKey)] = resaved
	return retrieveResp, resaved
}

func TestInvalidateStaleEntry_SkipsEntryResavedSinceRetrieve(t *testing.T) {
	mockClient := newMockAPIClient("s3")
	c := &client{api: mockClient, registry: "~"}
	retrieveResp, resaved := resavedEntry(mockClient)

	if c.invalidateStaleEntry(t.Context(), retrieveResp) {
		t.Error("invalidateStaleEntry() = true, want false when the entry was re-saved")
	}
	if len(mockClient.expireCalls) != 1 {
		t.Fatalf("expire calls = %d, want 1", len(mockClient.expireCalls))
	}
	if got := mockClient.expireCalls[0].UploadID; got != "stale-upload" {
		t.Errorf("expire request upload_id = %q, want %q", got, "stale-upload")
	}
	addr := cacheAddr(retrieveResp.TargetPaths, retrieveResp.CacheKey)
	if mockClient.registries["~"].cache[addr] != resaved {
		t.Error("re-saved entry was deleted, want it to survive")
	}
}

func TestConfirmRestoreSucceeded_DoesNotRefreshEntryResavedSinceRetrieve(t *testing.T) {
	mockClient := newMockAPIClient("s3")
	c := &client{api: mockClient, registry: "~"}
	retrieveResp, resaved := resavedEntry(mockClient)

	c.confirmRestoreSucceeded(t.Context(), retrieveResp, nil)

	if len(mockClient.confirmCalls) != 1 {
		t.Fatalf("confirm calls = %d, want 1", len(mockClient.confirmCalls))
	}
	if got := mockClient.confirmCalls[0].UploadID; got != "stale-upload" {
		t.Errorf("confirm request upload_id = %q, want %q", got, "stale-upload")
	}
	if !resaved.expiresAt.IsZero() {
		t.Errorf("re-saved entry retention refreshed to %v, want untouched", resaved.expiresAt)
	}
}

func TestConfirmRestoreSucceeded_EchoesScopesFromRetrieve(t *testing.T) {
	mockClient := newMockAPIClient("s3")
	c := &client{api: mockClient, registry: "~"}

	scopes := map[string]string{"branch": "main"}
	retrieveResp := api.CacheEntryRetrieveResp{
		TargetPaths: []string{"node_modules"},
		CacheKey:    []api.CacheKeyPart{{Value: "v1-test-key", Mandatory: true}},
		Scopes:      scopes,
		Fallback:    false,
	}

	confirmed := c.confirmRestoreSucceeded(t.Context(), retrieveResp, nil)
	if !confirmed {
		t.Fatalf("confirmRestoreSucceeded() = false, want true")
	}

	if len(mockClient.confirmCalls) != 1 {
		t.Fatalf("confirm calls = %d, want 1", len(mockClient.confirmCalls))
	}
	if got := mockClient.confirmCalls[0].Scopes; !reflect.DeepEqual(got, scopes) {
		t.Errorf("confirm request scopes = %+v, want %+v", got, scopes)
	}
}

// TestConfirmRestoreSucceeded_SkipsFallbackMatch mirrors the backend's
// (now-removed) `unless entry_result.fallback_used?` TTL-bump guard: a
// fallback hit means the entry was saved under a shorter key sequence, and
// confirming it would refresh a blob the caller didn't explicitly target.
func TestConfirmRestoreSucceeded_SkipsFallbackMatch(t *testing.T) {
	mockClient := newMockAPIClient("s3")
	c := &client{api: mockClient, registry: "~"}

	retrieveResp := api.CacheEntryRetrieveResp{
		TargetPaths: []string{"node_modules"},
		CacheKey:    []api.CacheKeyPart{{Value: "v1-test-key", Mandatory: true}},
		Fallback:    true,
	}

	confirmed := c.confirmRestoreSucceeded(t.Context(), retrieveResp, nil)
	if confirmed {
		t.Error("confirmRestoreSucceeded() = true, want false for a fallback match")
	}
	if len(mockClient.confirmCalls) != 0 {
		t.Errorf("confirm calls = %d, want 0 for a fallback match", len(mockClient.confirmCalls))
	}
}

// TestConfirmRestoreSucceeded_DeadlineBoundsEntireOperation guards against a
// regression where the timeout resets on every retry attempt instead of
// bounding the whole confirmRestoreSucceeded call. A confirmation request that
// hangs (network partition, blocked connection) must be cut off by
// confirmRestoreTimeout once, not retried up to roko's max attempts with a
// fresh timeout budget handed to each one.
func TestConfirmRestoreSucceeded_DeadlineBoundsEntireOperation(t *testing.T) {
	origTimeout := confirmRestoreTimeout
	confirmRestoreTimeout = 100 * time.Millisecond
	t.Cleanup(func() { confirmRestoreTimeout = origTimeout })

	mockClient := newMockAPIClient("s3")
	mockClient.confirmBlock = true
	c := &client{api: mockClient, registry: "~"}

	retrieveResp := api.CacheEntryRetrieveResp{
		TargetPaths: []string{"node_modules"},
		CacheKey:    []api.CacheKeyPart{{Value: "v1-test-key", Mandatory: true}},
	}

	start := time.Now()
	confirmed := c.confirmRestoreSucceeded(t.Context(), retrieveResp, nil)
	elapsed := time.Since(start)

	if confirmed {
		t.Error("confirmRestoreSucceeded() = true, want false for a request that never returns")
	}

	// A single blocked request should be cut off at roughly the deadline. If the
	// deadline instead reset per attempt, up to 5 attempts would each hang for
	// the full timeout, taking several times as long.
	if elapsed > confirmRestoreTimeout*2 {
		t.Errorf("confirmRestoreSucceeded() took %v, want close to the %v deadline (retries must not each get their own timeout)", elapsed, confirmRestoreTimeout)
	}
	if len(mockClient.confirmCalls) != 1 {
		t.Errorf("confirm calls = %d, want exactly 1 -- the deadline should expire before any retry is attempted", len(mockClient.confirmCalls))
	}
}

func TestConfirmRestoreSucceeded_MissingResolvedAddress(t *testing.T) {
	mockClient := newMockAPIClient("s3")
	c := &client{api: mockClient, registry: "~"}

	confirmed := c.confirmRestoreSucceeded(t.Context(), api.CacheEntryRetrieveResp{}, nil)
	if confirmed {
		t.Error("confirmRestoreSucceeded() = true, want false when the retrieve response has no resolved address")
	}
	if len(mockClient.confirmCalls) != 0 {
		t.Errorf("confirm calls = %d, want 0", len(mockClient.confirmCalls))
	}
}

// TestMissCompleteMessage guards the progress text agents/users see against
// claiming an invalidation happened when invalidateStaleEntry reported an
// idempotent no-op (existed: false).
func TestMissCompleteMessage(t *testing.T) {
	if got, want := missCompleteMessage("missing blob", true), "Cache miss (missing blob, invalidated stale entry)"; got != want {
		t.Errorf("missCompleteMessage(invalidated=true) = %q, want %q", got, want)
	}
	if got, want := missCompleteMessage("missing blob", false), "Cache miss (missing blob, entry may already have been removed or replaced by a newer save)"; got != want {
		t.Errorf("missCompleteMessage(invalidated=false) = %q, want %q", got, want)
	}
}

func TestCleanPath(t *testing.T) {
	t.Run("removes directory and contents", func(t *testing.T) {
		dir := t.TempDir()
		testDir := filepath.Join(dir, "cache")
		if err := os.MkdirAll(filepath.Join(testDir, "subdir"), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(filepath.Join(testDir, "file.txt"), []byte("test"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		if err := os.WriteFile(filepath.Join(testDir, "subdir", "nested.txt"), []byte("nested"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		if err := cleanPath(t.Context(), testDir); err != nil {
			t.Fatalf("cleanPath: %v", err)
		}

		_, err := os.Stat(testDir)
		if !os.IsNotExist(err) {
			t.Errorf("directory should be removed, got err=%v", err)
		}
	})

	t.Run("handles read-only directories (like go module cache)", func(t *testing.T) {
		dir := t.TempDir()
		testDir := filepath.Join(dir, "modcache")
		subdir := filepath.Join(testDir, "pkg")
		if err := os.MkdirAll(subdir, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(filepath.Join(subdir, "mod.go"), []byte("package mod"), 0o400); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		if err := os.Chmod(subdir, 0o555); err != nil {
			t.Fatalf("Chmod: %v", err)
		}
		if err := os.Chmod(testDir, 0o555); err != nil {
			t.Fatalf("Chmod: %v", err)
		}

		if err := cleanPath(t.Context(), testDir); err != nil {
			t.Fatalf("cleanPath: %v", err)
		}

		_, err := os.Stat(testDir)
		if !os.IsNotExist(err) {
			t.Errorf("directory should be removed, got err=%v", err)
		}
	})

	t.Run("succeeds on non-existent path", func(t *testing.T) {
		if err := cleanPath(t.Context(), "/nonexistent/path/that/does/not/exist"); err != nil {
			t.Fatalf("cleanPath: %v", err)
		}
	})

	t.Run("rejects empty path", func(t *testing.T) {
		err := cleanPath(t.Context(), "")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "empty directory path") {
			t.Errorf("error %q should contain %q", err.Error(), "empty directory path")
		}
	})

	t.Run("rejects root path", func(t *testing.T) {
		err := cleanPath(t.Context(), "/")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "refusing to remove") {
			t.Errorf("error %q should contain %q", err.Error(), "refusing to remove")
		}
	})

	t.Run("rejects current directory", func(t *testing.T) {
		err := cleanPath(t.Context(), ".")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "refusing to remove") {
			t.Errorf("error %q should contain %q", err.Error(), "refusing to remove")
		}
	})

	t.Run("rejects resolved current directory", func(t *testing.T) {
		// target_paths: ["."] resolves to the absolute cwd before cleanup; the
		// guard must catch that too, not just the literal ".".
		cwd, err := os.Getwd()
		if err != nil {
			t.Fatalf("os.Getwd: %v", err)
		}
		err = cleanPath(t.Context(), cwd)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "refusing to remove") {
			t.Errorf("error %q should contain %q", err.Error(), "refusing to remove")
		}
	})

	t.Run("rejects home directory", func(t *testing.T) {
		home, err := os.UserHomeDir()
		if err != nil {
			t.Fatalf("UserHomeDir: %v", err)
		}

		err = cleanPath(t.Context(), home)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "refusing to remove") {
			t.Errorf("error %q should contain %q", err.Error(), "refusing to remove")
		}
	})

	t.Run("removes a regular file", func(t *testing.T) {
		// A file target must be removable; makeTreeWritable's os.OpenRoot only
		// works on directories, so cleanPath must skip it for files.
		file := filepath.Join(t.TempDir(), "cache-file")
		if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		if err := cleanPath(t.Context(), file); err != nil {
			t.Fatalf("cleanPath: %v", err)
		}
		if _, err := os.Stat(file); !os.IsNotExist(err) {
			t.Errorf("file should be removed, stat err = %v", err)
		}
	})

	t.Run("removes a final symlink without following it to a protected dir", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("symlink creation needs privileges on Windows")
		}
		// A final symlink is only unlinked by RemoveAll, so removing it is safe
		// even when it points at the cwd — the referent must be left intact and
		// removal must not be refused (nor the referent's tree chmod-ed).
		cwd, err := os.Getwd()
		if err != nil {
			t.Fatalf("os.Getwd: %v", err)
		}
		link := filepath.Join(t.TempDir(), "alias")
		if err := os.Symlink(cwd, link); err != nil {
			t.Fatalf("Symlink: %v", err)
		}
		if err := cleanPath(t.Context(), link); err != nil {
			t.Fatalf("removing a symlink to cwd should be safe, got: %v", err)
		}
		if _, err := os.Lstat(link); !os.IsNotExist(err) {
			t.Errorf("symlink should be removed, got err=%v", err)
		}
		if _, err := os.Stat(cwd); err != nil {
			t.Errorf("cwd (the referent) must be left intact: %v", err)
		}
	})

	t.Run("rejects an ancestor of the working directory", func(t *testing.T) {
		// A target that contains the cwd (e.g. "/work" while cwd is "/work/job")
		// must be refused — RemoveAll would recursively delete the cwd too.
		base := t.TempDir()
		job := filepath.Join(base, "job")
		if err := os.MkdirAll(job, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		t.Chdir(job) // cwd is now base/job; restored automatically after the test

		err := cleanPath(t.Context(), base)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "refusing to remove") {
			t.Errorf("error %q should contain %q", err.Error(), "refusing to remove")
		}
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		dir := t.TempDir()
		testDir := filepath.Join(dir, "cache")
		if err := os.MkdirAll(testDir, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		err := cleanPath(ctx, testDir)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	})
}

// TestPathContains covers the ancestor/equality logic behind cleanPath's
// protected-directory guard.
func TestPathContains(t *testing.T) {
	base := t.TempDir()
	parent := filepath.Join(base, "work")
	child := filepath.Join(parent, "job")
	sibling := filepath.Join(base, "other")
	for _, d := range []string{child, sibling} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
	}

	parentInfo, err := os.Stat(parent)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	childInfo, err := os.Stat(child)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	if !pathContains(parentInfo, parent) {
		t.Error("a directory should contain itself")
	}
	if !pathContains(parentInfo, child) {
		t.Error("parent should contain its descendant")
	}
	if pathContains(parentInfo, sibling) {
		t.Error("parent should not contain a sibling")
	}
	if pathContains(childInfo, parent) {
		t.Error("child should not contain its parent")
	}
}

func TestCleanPathWindowsDriveRoot(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific test")
	}

	err := cleanPath(t.Context(), "C:\\")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "refusing to remove drive root") {
		t.Errorf("error %q should contain %q", err.Error(), "refusing to remove drive root")
	}
}

func TestCleanPathWindowsUNCShareRoot(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("UNC share roots are Windows-only")
	}

	// A UNC share root cleans to the volume name with no trailing separator, so
	// the drive-root guard misses it — cleanup must still refuse it.
	err := cleanPath(t.Context(), `\\server\share`)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "refusing to remove") {
		t.Errorf("error %q should contain %q", err.Error(), "refusing to remove")
	}
}

// fakeRefreshingBlob is a minimal store.Blob that also implements
// store.RetentionRefresher, mirroring NscStore/S3Blob.
type fakeRefreshingBlob struct {
	refreshCalls      []string
	refreshRetentions []time.Duration
	refresh           func(context.Context)
}

func (f *fakeRefreshingBlob) Upload(_ context.Context, _, _ string, _ time.Duration) (*store.TransferInfo, error) {
	return nil, nil
}

func (f *fakeRefreshingBlob) Download(_ context.Context, _, _ string) (*store.TransferInfo, error) {
	return nil, nil
}

func (f *fakeRefreshingBlob) Stat(_ context.Context, _ string) (int64, error) {
	return 0, nil
}

func (f *fakeRefreshingBlob) RefreshRetention(ctx context.Context, key string, retention time.Duration) {
	f.refreshCalls = append(f.refreshCalls, key)
	f.refreshRetentions = append(f.refreshRetentions, retention)
	if f.refresh != nil {
		f.refresh(ctx)
	}
}

// fakeNonRefreshingBlob is a store.Blob with no RefreshRetention method,
// mirroring LocalFileBlob (no retention concept to refresh).
type fakeNonRefreshingBlob struct{}

func (f *fakeNonRefreshingBlob) Upload(_ context.Context, _, _ string, _ time.Duration) (*store.TransferInfo, error) {
	return nil, nil
}

func (f *fakeNonRefreshingBlob) Download(_ context.Context, _, _ string) (*store.TransferInfo, error) {
	return nil, nil
}

func (f *fakeNonRefreshingBlob) Stat(_ context.Context, _ string) (int64, error) {
	return 0, nil
}

func TestMaybeRefreshRetention(t *testing.T) {
	t.Run("bounds a stalled refresh without canceling the caller", func(t *testing.T) {
		for _, parentLimit := range []time.Duration{time.Hour, time.Second} {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), parentLimit)
				defer cancel()
				blob := &fakeRefreshingBlob{refresh: func(ctx context.Context) { <-ctx.Done() }}
				start := time.Now()
				maybeRefreshRetention(ctx, blob, false, "key", time.Hour)
				if elapsed := time.Since(start); elapsed != min(parentLimit, 5*time.Second) {
					t.Fatalf("stalled refresh took %v", elapsed)
				}
				if parentLimit > 5*time.Second && ctx.Err() != nil {
					t.Fatalf("refresh canceled caller: %v", ctx.Err())
				}
			})
		}
	})

	t.Run("refreshes on an exact match with the given retention", func(t *testing.T) {
		blob := &fakeRefreshingBlob{}

		maybeRefreshRetention(t.Context(), blob, false, "key", 48*time.Hour)

		if len(blob.refreshCalls) != 1 {
			t.Errorf("refresh calls = %d, want 1", len(blob.refreshCalls))
		}
		if len(blob.refreshRetentions) != 1 || blob.refreshRetentions[0] != 48*time.Hour {
			t.Errorf("refresh retentions = %v, want [48h]", blob.refreshRetentions)
		}
	})

	// Mirrors the backend's `unless entry_result.fallback_used?` TTL-bump
	// guard: a fallback match must not refresh the blob's retention.
	t.Run("skips the refresh on a fallback match", func(t *testing.T) {
		blob := &fakeRefreshingBlob{}

		maybeRefreshRetention(t.Context(), blob, true, "key", 48*time.Hour)

		if len(blob.refreshCalls) != 0 {
			t.Errorf("refresh calls = %d, want 0 for a fallback match", len(blob.refreshCalls))
		}
	})

	t.Run("does nothing for a store with no retention concept", func(t *testing.T) {
		blob := &fakeNonRefreshingBlob{}

		maybeRefreshRetention(t.Context(), blob, false, "key", 48*time.Hour) // must not panic
	})
}

func TestRunRestoreDownloadTimeout(t *testing.T) {
	for _, scenario := range []string{"timeout", "strict timeout", "job cancellation", "default off"} {
		t.Run(scenario, func(t *testing.T) {
			reg, apiClient, cfg := setupExecTest(t)
			writeFile(t, "out/file", "cached")
			if err := RunSave(t.Context(), logger.Discard, apiClient, cfg); err != nil {
				t.Fatal(err)
			}
			writeFile(t, "out/file", "original")
			blobURL, _ := url.Parse(cfg.BucketURL)
			storageRoot := filepath.FromSlash(blobURL.Path)
			if runtime.GOOS == "windows" {
				storageRoot = strings.TrimPrefix(storageRoot, `\`)
			}
			reg.mu.Lock()
			for key, entry := range reg.entries {
				entry.Store = store.AgentManaged
				entry.Fallback = true // No TTL refresh; the remote entry must stay intact.
				reg.entries[key] = entry
			}
			reg.mu.Unlock()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			stopped := make(chan struct{})
			blobServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(stopped)
				data, err := os.ReadFile(filepath.Join(storageRoot, filepath.Base(r.URL.Path)))
				if err != nil {
					t.Error(err)
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Length", fmt.Sprint(len(data)))
				_, _ = w.Write(data[:1])
				w.(http.Flusher).Flush()
				switch scenario {
				case "job cancellation":
					cancel()
				case "default off":
					time.Sleep(50 * time.Millisecond)
					_, _ = w.Write(data[1:])
					return
				}
				<-r.Context().Done()
			}))
			defer blobServer.Close()
			t.Setenv("AWS_ACCESS_KEY_ID", "test")
			t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
			cfg.BucketURL = "s3://bucket?use_path_style=true&endpoint=" + blobServer.URL
			cfg.RestoreDownloadTimeout = 200 * time.Millisecond
			cfg.FailOnError = scenario == "strict timeout"
			switch scenario {
			case "default off":
				cfg.RestoreDownloadTimeout = 0
			case "job cancellation":
				cfg.RestoreDownloadTimeout = time.Hour
			}
			tempRoot := t.TempDir()
			for _, name := range []string{"TMPDIR", "TMP", "TEMP"} {
				t.Setenv(name, tempRoot)
			}
			err := RunRestore(ctx, logger.Discard, apiClient, cfg)
			switch scenario {
			case "strict timeout":
				if !errors.Is(err, errDownloadTimeout) {
					t.Fatalf("strict timeout = %v", err)
				}
			case "job cancellation":
				if !errors.Is(err, context.Canceled) || errors.Is(err, errDownloadTimeout) {
					t.Fatalf("job cancellation = %v", err)
				}
			default:
				if err != nil {
					t.Fatal(err)
				}
			}
			<-stopped
			want := "original"
			if scenario == "default off" {
				want = "cached"
			}
			if got := readFile(t, "out/file"); got != want {
				t.Fatalf("target = %q, want %q", got, want)
			}
			entries, err := os.ReadDir(tempRoot)
			if err != nil || len(entries) != 0 {
				t.Fatalf("temporary download remains after writer shutdown: %v, %v", entries, err)
			}
			reg.mu.Lock()
			defer reg.mu.Unlock()
			if len(reg.entries) != 1 {
				t.Fatal("download timeout expired remote entry")
			}
		})
	}
}

func TestRunRestoreDownloadTimeoutSharedAcrossCaches(t *testing.T) {
	for _, concurrency := range []int{1, 2} {
		t.Run(fmt.Sprint("concurrency=", concurrency), func(t *testing.T) {
			reg, apiClient, cfg := setupExecTest(t)
			writeFile(t, "cache.yml", `caches:
  - {name: one, cache_key: [one], target_paths: [one]}
  - {name: two, cache_key: [two], target_paths: [two]}
  - {name: three, cache_key: [three], target_paths: [three]}
`)
			cfg.Names = nil
			cfg.Concurrency = concurrency
			writeFile(t, "one/file", "cached one")
			writeFile(t, "two/file", "cached two")
			writeFile(t, "three/file", "cached three")
			if err := RunSave(t.Context(), logger.Discard, apiClient, cfg); err != nil {
				t.Fatal(err)
			}
			blobs := make(map[string][]byte)
			blobStore, err := store.NewBlobStore(t.Context(), store.LocalFileStore, cfg.BucketURL)
			if err != nil {
				t.Fatal(err)
			}
			reg.mu.Lock()
			for key, entry := range reg.entries {
				digest := entry.Blobs[0].Digest.Value
				path := filepath.Join(t.TempDir(), "blob")
				if _, err := blobStore.Download(t.Context(), digest, path); err != nil {
					t.Fatal(err)
				}
				blobs[digest], err = os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				entry.Store, entry.Fallback = store.AgentManaged, true
				reg.entries[key] = entry
			}
			reg.mu.Unlock()
			writeFile(t, "one/file", "original one")
			writeFile(t, "two/file", "original two")
			writeFile(t, "three/file", "original three")
			var downloads, lookups atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.URL.Path, "/cache_registries/") {
					if concurrency == 1 && strings.HasSuffix(r.URL.Path, "/retrieve") && lookups.Add(1) == 2 {
						// A pause longer than the allowance must not consume download time.
						time.Sleep(3 * time.Second)
					}
					reg.ServeHTTP(w, r)
					return
				}
				downloads.Add(1)
				data := blobs[filepath.Base(r.URL.Path)]
				w.Header().Set("Content-Length", fmt.Sprint(len(data)))
				_, _ = w.Write(data[:1])
				w.(http.Flusher).Flush()
				select {
				case <-time.After(1200 * time.Millisecond):
					_, _ = w.Write(data[1:])
				case <-r.Context().Done():
				}
			}))
			defer server.Close()
			t.Setenv("AWS_ACCESS_KEY_ID", "test")
			t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
			cfg.BucketURL = "s3://bucket?use_path_style=true&endpoint=" + server.URL
			cfg.RestoreDownloadTimeout = 2 * time.Second
			apiClient = api.NewClient(logger.Discard, api.Config{Endpoint: server.URL})
			if err := RunRestore(t.Context(), logger.Discard, apiClient, cfg); err != nil {
				t.Fatal(err)
			}
			if want := int32(concurrency + 1); downloads.Load() != want {
				t.Fatalf("download calls = %d, want %d (no download starts after allowance is exhausted)", downloads.Load(), want)
			}
			if got := readFile(t, "one/file"); got != "cached one" {
				t.Fatalf("completed cache = %q, want cached one", got)
			}
			want := "original two" // Serial downloads share 2s; each needs 1.2s.
			if concurrency == 2 {
				want = "cached two" // Overlapping downloads count once, not 2.4s.
			}
			if got := readFile(t, "two/file"); got != want {
				t.Fatalf("later cache = %q, want %q", got, want)
			}
			if got := readFile(t, "three/file"); got != "original three" {
				t.Fatalf("exhausted allowance was renewed for the third cache: %q", got)
			}
		})
	}
}

func TestRestoreDownloadTimeoutDoesNotLimitExtraction(t *testing.T) {
	for _, jobCancel := range []bool{false, true} {
		t.Run(fmt.Sprint("job cancellation=", jobCancel), func(t *testing.T) {
			_, apiClient, cfg := setupExecTest(t)
			writeFile(t, "out/file", "cached")
			if err := RunSave(t.Context(), logger.Discard, apiClient, cfg); err != nil {
				t.Fatal(err)
			}
			writeFile(t, "out/file", "original")
			c, ids, err := newClient(logger.Discard, apiClient, cfg)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			c.downloadBudget = &downloadBudget{remaining: 200 * time.Millisecond}
			c.onProgress = func(_, stage, _ string, _, _ int) {
				if stage == "extracting" {
					time.Sleep(400 * time.Millisecond)
					if jobCancel {
						cancel()
					}
				}
			}
			err = restoreWithClient(ctx, logger.Discard, c, ids, 1, true)
			if jobCancel {
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("extraction job cancellation = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("completed download inherited a deadline during extraction: %v", err)
			}
			if got := readFile(t, "out/file"); got != "cached" {
				t.Fatalf("target = %q, want cached", got)
			}
		})
	}
}
