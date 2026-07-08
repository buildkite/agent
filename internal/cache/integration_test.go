package cache

import (
	"context"
	"crypto/rand"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/internal/cache/configuration"
)

// mockAPIClient implements api.CacheClient for integration testing
type mockAPIClient struct {
	registries map[string]*mockRegistry
	// expireCalls records the addresses passed to CacheEntryExpire
	expireCalls []api.CacheEntryExpireReq
}

type mockRegistry struct {
	name  string
	store string
	cache map[string]*mockCacheEntry
}

type mockCacheEntry struct {
	targetPaths     []string
	cacheKey        []api.CacheKeyPart
	blobs           []api.CacheBlob
	storeObjectName string
	uploadID        string
	committed       bool
	expiresAt       time.Time
	platform        string
}

// cacheAddr builds the v2 entry address from the order-insensitive target_paths
// set and the ordered cache_key, mirroring how the backend composes its key.
func cacheAddr(targetPaths []string, cacheKey []api.CacheKeyPart) string {
	paths := append([]string(nil), targetPaths...)
	sort.Strings(paths)

	values := make([]string, len(cacheKey))
	for i, p := range cacheKey {
		values[i] = p.Value
	}
	return strings.Join(paths, ",") + "#" + strings.Join(values, "#")
}

func newMockAPIClient(storageType string) *mockAPIClient {
	return &mockAPIClient{
		registries: map[string]*mockRegistry{
			"~": {
				name:  "~",
				store: storageType,
				cache: make(map[string]*mockCacheEntry),
			},
		},
	}
}

func (m *mockAPIClient) Do(req *http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockAPIClient) CacheRegistry(ctx context.Context, registry string) (api.CacheRegistryResp, *api.Response, error) {
	reg, ok := m.registries[registry]
	if !ok {
		return api.CacheRegistryResp{}, nil, fmt.Errorf("registry not found: %s", registry)
	}

	return api.CacheRegistryResp{
		Name:  reg.name,
		Store: reg.store,
	}, nil, nil
}

func (m *mockAPIClient) CacheEntryPeekExists(ctx context.Context, registry string, req api.CacheEntryPeekReq) (api.CacheEntryPeekResp, bool, *api.Response, error) {
	reg, ok := m.registries[registry]
	if !ok {
		return api.CacheEntryPeekResp{}, false, nil, fmt.Errorf("registry not found: %s", registry)
	}

	entry, exists := reg.cache[cacheAddr(req.TargetPaths, req.CacheKey)]
	if !exists || !entry.committed {
		return api.CacheEntryPeekResp{Message: api.CacheEntryNotFound}, false, nil, nil
	}

	return api.CacheEntryPeekResp{
		Store:       reg.store,
		TargetPaths: entry.targetPaths,
		CacheKey:    entry.cacheKey,
		Blobs:       entry.blobs,
	}, true, nil, nil
}

func (m *mockAPIClient) CacheEntryCreate(ctx context.Context, registry string, req api.CacheEntryCreateReq) (api.CacheEntryCreateResp, *api.Response, error) {
	reg, ok := m.registries[registry]
	if !ok {
		return api.CacheEntryCreateResp{}, nil, fmt.Errorf("registry not found: %s", registry)
	}

	uploadID := fmt.Sprintf("upload-%d", time.Now().UnixNano())

	// Content-addressed storage: the object name is the blob digest.
	var storeObjectName string
	if len(req.Blobs) > 0 {
		storeObjectName = req.Blobs[0].Digest.Value
	}

	entry := &mockCacheEntry{
		targetPaths:     req.TargetPaths,
		cacheKey:        req.CacheKey,
		blobs:           req.Blobs,
		storeObjectName: storeObjectName,
		uploadID:        uploadID,
		committed:       false,
		expiresAt:       time.Now().Add(7 * 24 * time.Hour),
		platform:        req.Platform,
	}

	reg.cache[cacheAddr(req.TargetPaths, req.CacheKey)] = entry

	return api.CacheEntryCreateResp{
		UploadID: uploadID,
	}, nil, nil
}

func (m *mockAPIClient) CacheEntryCommit(ctx context.Context, registry string, req api.CacheEntryCommitReq) (api.CacheEntryCommitResp, *api.Response, error) {
	reg, ok := m.registries[registry]
	if !ok {
		return api.CacheEntryCommitResp{}, nil, fmt.Errorf("registry not found: %s", registry)
	}

	for _, entry := range reg.cache {
		if entry.uploadID == req.UploadID {
			entry.committed = true
			return api.CacheEntryCommitResp{Message: "Cache entry committed successfully"}, nil, nil
		}
	}

	return api.CacheEntryCommitResp{}, nil, fmt.Errorf("upload ID not found: %s", req.UploadID)
}

func (m *mockAPIClient) CacheEntryRetrieve(ctx context.Context, registry string, req api.CacheEntryRetrieveReq) (api.CacheEntryRetrieveResp, bool, *api.Response, error) {
	reg, ok := m.registries[registry]
	if !ok {
		return api.CacheEntryRetrieveResp{}, false, nil, fmt.Errorf("registry not found: %s", registry)
	}

	// fallback walking will be implemented in the future, for now we only support exact key matches.
	if entry, exists := reg.cache[cacheAddr(req.TargetPaths, req.CacheKey)]; exists && entry.committed {
		return api.CacheEntryRetrieveResp{
			Store:       reg.store,
			TargetPaths: entry.targetPaths,
			CacheKey:    entry.cacheKey,
			Blobs:       entry.blobs,
			Fallback:    false,
			ExpiresAt:   entry.expiresAt,
		}, true, nil, nil
	}

	return api.CacheEntryRetrieveResp{Message: api.CacheEntryNotFound}, false, nil, nil
}

func (m *mockAPIClient) CacheEntryExpire(ctx context.Context, registry string, req api.CacheEntryExpireReq) (*api.Response, error) {
	m.expireCalls = append(m.expireCalls, req)

	reg, ok := m.registries[registry]
	if !ok {
		return nil, fmt.Errorf("registry not found: %s", registry)
	}

	// Mirror the backend's delete_item so a subsequent save
	// re-uploads the invalidated entry.
	delete(reg.cache, cacheAddr(req.TargetPaths, req.CacheKey))
	return nil, nil
}

// createRandomFile creates a file filled with random data
func createRandomFile(t *testing.T, path string, sizeBytes int64) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer func() { _ = f.Close() }()

	// Write random data in chunks to avoid memory issues with large files
	const chunkSize = 1024 * 1024 // 1MB chunks
	buf := make([]byte, chunkSize)
	remaining := sizeBytes

	for remaining > 0 {
		toWrite := min(remaining, int64(chunkSize))

		if _, err := rand.Read(buf[:toWrite]); err != nil {
			t.Fatalf("rand.Read: %v", err)
		}

		if _, err := f.Write(buf[:toWrite]); err != nil {
			t.Fatalf("Write: %v", err)
		}

		remaining -= toWrite
	}

	if err := f.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}
}

// setupTestCache creates a test cache with temporary directories and files
func setupTestCache(t *testing.T, storageType string) (cacheClient *client, cacheDir, storageDir string) {
	t.Helper()

	// Create temp directory under current working directory to satisfy chroot requirements
	// The archive code uses UserHomeDir as chroot for absolute paths, but CWD for relative paths
	// Using a relative path ensures it's within the CWD chroot
	tmpBase := filepath.Join(".test-cache", t.Name())
	t.Cleanup(func() {
		_ = os.RemoveAll(".test-cache")
	})
	cacheDir = filepath.Join(tmpBase, "cache")
	storageDir = filepath.Join(tmpBase, "storage")

	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.MkdirAll(storageDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Create test files with random data (~100MB total)
	testFiles := []string{
		filepath.Join(cacheDir, "large-file-1.bin"),
		filepath.Join(cacheDir, "large-file-2.bin"),
		filepath.Join(cacheDir, "nested", "large-file-3.bin"),
	}

	for _, file := range testFiles {
		// Create ~33MB files so total is ~100MB
		createRandomFile(t, file, 33*1024*1024)
	}

	// Create mock API client
	mockClient := newMockAPIClient(storageType)

	// Build storage URL based on type (need absolute path for file:// URLs).
	// On Windows, paths like "C:\foo" must be encoded as "/C:/foo" so the
	// resulting URL is the well-formed "file:///C:/foo".
	absStorageDir, err := filepath.Abs(storageDir)
	if err != nil {
		t.Fatalf("filepath.Abs: %v", err)
	}

	urlPath := filepath.ToSlash(absStorageDir)
	if !strings.HasPrefix(urlPath, "/") {
		urlPath = "/" + urlPath
	}

	var bucketURL string
	switch storageType {
	case "local_file":
		bucketURL = fmt.Sprintf("file://%s", urlPath)
	default:
		t.Fatalf("unsupported storage type: %s", storageType)
	}

	// Create cache client
	c := &client{
		api:       mockClient,
		bucketURL: bucketURL,
		format:    "zip",
		platform:  "linux/amd64",
		registry:  "~",
		caches: []configuration.Cache{
			{
				Name:        "test-cache",
				CacheKey:    []configuration.KeyPart{{Source: configuration.SourceLiteral, Arg: "v1-test-key"}},
				TargetPaths: []string{cacheDir},
			},
		},
		onProgress: nil,
	}

	return c, cacheDir, storageDir
}

func TestCacheIntegration_SaveAndRestore(t *testing.T) {
	ctx := t.Context()

	// Setup test cache with local file storage
	cacheClient, cacheDir, storageDir := setupTestCache(t, "local_file")

	// Save the cache
	t.Run("save", func(t *testing.T) {
		result, err := cacheClient.Save(ctx, "test-cache")
		if err != nil {
			t.Fatalf("Save: %v", err)
		}

		if !result.CacheEntryCreated {
			t.Error("cache should be created")
		}
		if result.Key != "test-cache" {
			t.Errorf("Key = %q, want %q", result.Key, "test-cache")
		}
		if result.UploadID == "" {
			t.Error("UploadID should not be empty")
		}
		if result.Archive.Size <= 0 {
			t.Errorf("archive should have size, got %d", result.Archive.Size)
		}
		if result.Archive.WrittenBytes <= 0 {
			t.Errorf("should have written bytes, got %d", result.Archive.WrittenBytes)
		}
		if result.Archive.WrittenEntries <= 0 {
			t.Errorf("should have entries, got %d", result.Archive.WrittenEntries)
		}
		if result.Archive.Sha256Sum == "" {
			t.Error("should have SHA256 checksum")
		}
		if result.Transfer == nil {
			t.Fatal("should have transfer info")
		}
		if result.Transfer.BytesTransferred <= 0 {
			t.Errorf("should have transferred bytes, got %d", result.Transfer.BytesTransferred)
		}
		if result.TotalDuration <= 0 {
			t.Errorf("should have duration, got %v", result.TotalDuration)
		}

		t.Logf("Saved cache: %d bytes (%.2f MB), compression ratio: %.2fx, duration: %s",
			result.Archive.Size,
			float64(result.Archive.Size)/(1024*1024),
			result.Archive.CompressionRatio,
			result.TotalDuration)
	})

	// Verify the cache was stored in the storage backend
	t.Run("verify_storage", func(t *testing.T) {
		// For local_file storage, check that files exist
		entries, err := os.ReadDir(storageDir)
		if err != nil {
			t.Fatalf("ReadDir: %v", err)
		}
		if len(entries) == 0 {
			t.Error("storage directory should contain files")
		}
	})

	// Remove the original cache files to simulate a fresh checkout
	t.Run("cleanup_cache_dir", func(t *testing.T) {
		if err := os.RemoveAll(cacheDir); err != nil {
			t.Fatalf("RemoveAll: %v", err)
		}
		if err := os.MkdirAll(cacheDir, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}

		// Verify directory is empty
		entries, err := os.ReadDir(cacheDir)
		if err != nil {
			t.Fatalf("ReadDir: %v", err)
		}
		if len(entries) != 0 {
			t.Errorf("cache directory should be empty, got %d entries", len(entries))
		}
	})

	// Restore the cache
	t.Run("restore", func(t *testing.T) {
		result, err := cacheClient.Restore(ctx, "test-cache")
		if err != nil {
			t.Fatalf("Restore: %v", err)
		}

		if !result.CacheRestored {
			t.Error("cache should be restored")
		}
		if !result.CacheHit {
			t.Error("should be exact key match")
		}
		if result.FallbackUsed {
			t.Error("should not use fallback")
		}
		if result.Key != "test-cache" {
			t.Errorf("Key = %q, want %q", result.Key, "test-cache")
		}
		if result.Archive.Size <= 0 {
			t.Errorf("archive should have size, got %d", result.Archive.Size)
		}
		if result.Archive.WrittenBytes <= 0 {
			t.Errorf("should have written bytes, got %d", result.Archive.WrittenBytes)
		}
		if result.Archive.WrittenEntries <= 0 {
			t.Errorf("should have entries, got %d", result.Archive.WrittenEntries)
		}
		if result.Transfer.BytesTransferred <= 0 {
			t.Errorf("should have transferred bytes, got %d", result.Transfer.BytesTransferred)
		}
		if result.TotalDuration <= 0 {
			t.Errorf("should have duration, got %v", result.TotalDuration)
		}

		t.Logf("Restored cache: %d bytes (%.2f MB), compression ratio: %.2fx, duration: %s",
			result.Archive.Size,
			float64(result.Archive.Size)/(1024*1024),
			result.Archive.CompressionRatio,
			result.TotalDuration)
	})

	// Verify the restored files match the original files
	t.Run("verify_restored_files", func(t *testing.T) {
		expectedFiles := []string{
			filepath.Join(cacheDir, "large-file-1.bin"),
			filepath.Join(cacheDir, "large-file-2.bin"),
			filepath.Join(cacheDir, "nested", "large-file-3.bin"),
		}

		for _, file := range expectedFiles {
			stat, err := os.Stat(file)
			if err != nil {
				t.Errorf("restored file should exist: %s: %v", file, err)
				continue
			}

			// Verify file size is approximately correct (~33MB each)
			const expected = int64(33 * 1024 * 1024)
			const delta = int64(1024 * 1024)
			if math.Abs(float64(stat.Size()-expected)) > float64(delta) {
				t.Errorf("file %s size should be ~33MB, got %d", file, stat.Size())
			}
		}
	})
}

func TestCacheIntegration_SaveAlreadyExists(t *testing.T) {
	ctx := t.Context()

	cacheClient, _, _ := setupTestCache(t, "local_file")

	// Save the cache first time
	result1, err := cacheClient.Save(ctx, "test-cache")
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !result1.CacheEntryCreated {
		t.Error("first save should create cache")
	}

	// Try to save again
	result2, err := cacheClient.Save(ctx, "test-cache")
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if result2.CacheEntryCreated {
		t.Error("second save should not create cache")
	}
	if result2.Transfer != nil {
		t.Error("should not have transfer info when cache exists")
	}
	if result2.Key != "test-cache" {
		t.Errorf("Key = %q, want %q", result2.Key, "test-cache")
	}
}

func TestCacheIntegration_RestoreCacheMiss(t *testing.T) {
	ctx := t.Context()

	cacheClient, _, _ := setupTestCache(t, "local_file")

	// Try to restore without saving first
	result, err := cacheClient.Restore(ctx, "test-cache")
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}

	if result.CacheRestored {
		t.Error("should not restore when cache doesn't exist")
	}
	if result.CacheHit {
		t.Error("should not be a cache hit")
	}
	if result.FallbackUsed {
		t.Error("should not use fallback")
	}
	if result.Key != "test-cache" {
		t.Errorf("should return requested key, Key = %q, want %q", result.Key, "test-cache")
	}
}

// TestCacheIntegration_RestoreMissingBlobInvalidates exercises the split-brain
// recovery path: the entry still exists in the registry but its backing blob is
// gone. Restore must degrade to a cache miss, invalidate the stale entry,
// and let a subsequent save re-upload it.
func TestCacheIntegration_RestoreMissingBlobInvalidates(t *testing.T) {
	ctx := t.Context()

	cacheClient, _, storageDir := setupTestCache(t, "local_file")
	mockClient := cacheClient.api.(*mockAPIClient)

	// Save so the entry is committed and the blob exists.
	saveResult, err := cacheClient.Save(ctx, "test-cache")
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !saveResult.CacheEntryCreated {
		t.Fatal("expected initial save to create an entry")
	}

	// Simulate the blob being lifecycle/TTL-deleted while the entry survives.
	blobEntries, err := os.ReadDir(storageDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range blobEntries {
		if err := os.RemoveAll(filepath.Join(storageDir, e.Name())); err != nil {
			t.Fatalf("RemoveAll: %v", err)
		}
	}

	// Restore must not error, must report a miss, and must invalidate the entry.
	restoreResult, err := cacheClient.Restore(ctx, "test-cache")
	if err != nil {
		t.Fatalf("Restore with missing blob should not error, got: %v", err)
	}
	if restoreResult.CacheRestored {
		t.Error("missing blob should degrade to CacheRestored=false")
	}
	if restoreResult.CacheHit {
		t.Error("missing blob should not be a cache hit")
	}

	// Exactly one expire, targeting the resolved entry address.
	if len(mockClient.expireCalls) != 1 {
		t.Fatalf("expire calls = %d, want 1", len(mockClient.expireCalls))
	}
	got := mockClient.expireCalls[0]
	if len(got.CacheKey) != 1 || got.CacheKey[0].Value != "v1-test-key" {
		t.Errorf("expire targeted cache_key %+v, want single part v1-test-key", got.CacheKey)
	}

	// A subsequent save must re-upload, proving the entry was invalidated.
	resaveResult, err := cacheClient.Save(ctx, "test-cache")
	if err != nil {
		t.Fatalf("re-Save: %v", err)
	}
	if !resaveResult.CacheEntryCreated {
		t.Error("expected re-save to re-create the invalidated entry")
	}
}

// TODO: restore fallback-matching coverage (was TestCacheIntegration_RestoreWithFallback,
// removed in the v2 migration) once agent-side fallbackLimit parsing lands and the agent
// sends mandatory:false parts. Until then the agent only addresses exact matches.

func TestCacheIntegration_LargeFileChecksum(t *testing.T) {
	ctx := t.Context()

	cacheClient, cacheDir, _ := setupTestCache(t, "local_file")

	// Save cache
	result, err := cacheClient.Save(ctx, "test-cache")
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !result.CacheEntryCreated {
		t.Fatal("expected CacheEntryCreated to be true")
	}

	checksum1 := result.Archive.Sha256Sum
	if checksum1 == "" {
		t.Error("should have SHA256 checksum")
	}

	// Restore and save again
	if err := os.RemoveAll(cacheDir); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	if _, err = cacheClient.Restore(ctx, "test-cache"); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	// Save again - should be detected as already exists
	result2, err := cacheClient.Save(ctx, "test-cache")
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if result2.CacheEntryCreated {
		t.Error("cache should already exist")
	}
}

func TestCacheIntegration_TransferMetrics(t *testing.T) {
	ctx := t.Context()

	cacheClient, _, _ := setupTestCache(t, "local_file")

	// Save cache and check transfer metrics
	saveResult, err := cacheClient.Save(ctx, "test-cache")
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if saveResult.Transfer == nil {
		t.Fatal("expected Transfer to be non-nil")
	}

	if saveResult.Transfer.BytesTransferred <= 0 {
		t.Errorf("BytesTransferred should be > 0, got %d", saveResult.Transfer.BytesTransferred)
	}
	if saveResult.Transfer.TransferSpeed <= 0.0 {
		t.Errorf("TransferSpeed should be > 0, got %f", saveResult.Transfer.TransferSpeed)
	}
	if saveResult.Transfer.Duration <= 0 {
		t.Errorf("Duration should be > 0, got %v", saveResult.Transfer.Duration)
	}

	t.Logf("Upload: %d bytes at %.2f MB/s in %s",
		saveResult.Transfer.BytesTransferred,
		saveResult.Transfer.TransferSpeed,
		saveResult.Transfer.Duration)

	// Restore cache and check transfer metrics
	restoreResult, err := cacheClient.Restore(ctx, "test-cache")
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}

	if restoreResult.Transfer.BytesTransferred <= 0 {
		t.Errorf("BytesTransferred should be > 0, got %d", restoreResult.Transfer.BytesTransferred)
	}
	if restoreResult.Transfer.TransferSpeed <= 0.0 {
		t.Errorf("TransferSpeed should be > 0, got %f", restoreResult.Transfer.TransferSpeed)
	}
	if restoreResult.Transfer.Duration <= 0 {
		t.Errorf("Duration should be > 0, got %v", restoreResult.Transfer.Duration)
	}

	t.Logf("Download: %d bytes at %.2f MB/s in %s",
		restoreResult.Transfer.BytesTransferred,
		restoreResult.Transfer.TransferSpeed,
		restoreResult.Transfer.Duration)
}
