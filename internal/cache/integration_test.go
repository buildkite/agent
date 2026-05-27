package cache

import (
	"context"
	"crypto/rand"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/internal/cache/configuration"
)

// mockAPIClient implements api.CacheClient for integration testing
type mockAPIClient struct {
	registries map[string]*mockRegistry
}

type mockRegistry struct {
	name  string
	store string
	cache map[string]*mockCacheEntry
}

type mockCacheEntry struct {
	key             string
	storeObjectName string
	uploadID        string
	digest          string
	compression     string
	fileSize        int
	committed       bool
	expiresAt       time.Time
	fallbackKeys    []string
	paths           []string
	platform        string
	pipeline        string
	branch          string
	organization    string
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

func (m *mockAPIClient) CacheRegistry(ctx context.Context, registry string) (api.CacheRegistryResp, error) {
	reg, ok := m.registries[registry]
	if !ok {
		return api.CacheRegistryResp{}, fmt.Errorf("registry not found: %s", registry)
	}

	return api.CacheRegistryResp{
		Name:  reg.name,
		Store: reg.store,
	}, nil
}

func (m *mockAPIClient) CachePeekExists(ctx context.Context, registry string, req api.CachePeekReq) (api.CachePeekResp, bool, error) {
	reg, ok := m.registries[registry]
	if !ok {
		return api.CachePeekResp{}, false, fmt.Errorf("registry not found: %s", registry)
	}

	entry, exists := reg.cache[req.Key]
	if !exists || !entry.committed {
		return api.CachePeekResp{Message: api.CacheEntryNotFound}, false, nil
	}

	return api.CachePeekResp{
		Store:        reg.store,
		Digest:       entry.digest,
		ExpiresAt:    entry.expiresAt,
		Compression:  entry.compression,
		FileSize:     entry.fileSize,
		Paths:        entry.paths,
		Pipeline:     entry.pipeline,
		Branch:       entry.branch,
		Owner:        entry.organization,
		Platform:     entry.platform,
		Key:          entry.key,
		FallbackKeys: entry.fallbackKeys,
		CreatedAt:    entry.expiresAt.Add(-7 * 24 * time.Hour), // 7 days before expiry
		AgentID:      "test-agent-id",
		JobID:        "test-job-id",
		BuildID:      "test-build-id",
	}, true, nil
}

func (m *mockAPIClient) CacheCreate(ctx context.Context, registry string, req api.CacheCreateReq) (api.CacheCreateResp, error) {
	reg, ok := m.registries[registry]
	if !ok {
		return api.CacheCreateResp{}, fmt.Errorf("registry not found: %s", registry)
	}

	uploadID := fmt.Sprintf("upload-%d", time.Now().UnixNano())
	storeObjectName := fmt.Sprintf("%s/%s/%s/%s", req.Organization, req.Pipeline, req.Branch, req.Key)

	entry := &mockCacheEntry{
		key:             req.Key,
		storeObjectName: storeObjectName,
		uploadID:        uploadID,
		digest:          req.Digest,
		compression:     req.Compression,
		fileSize:        req.FileSize,
		committed:       false,
		expiresAt:       time.Now().Add(7 * 24 * time.Hour),
		fallbackKeys:    req.FallbackKeys,
		paths:           req.Paths,
		platform:        req.Platform,
		pipeline:        req.Pipeline,
		branch:          req.Branch,
		organization:    req.Organization,
	}

	reg.cache[req.Key] = entry

	return api.CacheCreateResp{
		UploadID:        uploadID,
		StoreObjectName: storeObjectName,
	}, nil
}

func (m *mockAPIClient) CacheCommit(ctx context.Context, registry string, req api.CacheCommitReq) (api.CacheCommitResp, error) {
	reg, ok := m.registries[registry]
	if !ok {
		return api.CacheCommitResp{}, fmt.Errorf("registry not found: %s", registry)
	}

	for _, entry := range reg.cache {
		if entry.uploadID == req.UploadID {
			entry.committed = true
			return api.CacheCommitResp{Message: "Cache entry committed successfully"}, nil
		}
	}

	return api.CacheCommitResp{}, fmt.Errorf("upload ID not found: %s", req.UploadID)
}

func (m *mockAPIClient) CacheRetrieve(ctx context.Context, registry string, req api.CacheRetrieveReq) (api.CacheRetrieveResp, bool, error) {
	reg, ok := m.registries[registry]
	if !ok {
		return api.CacheRetrieveResp{}, false, fmt.Errorf("registry not found: %s", registry)
	}

	// Try exact key match first
	if entry, exists := reg.cache[req.Key]; exists && entry.committed {
		return api.CacheRetrieveResp{
			Store:           reg.store,
			Key:             entry.key,
			Fallback:        false,
			StoreObjectName: entry.storeObjectName,
			ExpiresAt:       entry.expiresAt,
			CompressionType: entry.compression,
		}, true, nil
	}

	// Try fallback keys - check if the entry key matches any of the requested fallback keys
	if req.FallbackKeys != "" {
		// FallbackKeys is a comma-separated string
		fallbackKeys := strings.Split(req.FallbackKeys, ",")
		for _, fbKey := range fallbackKeys {
			fbKey = strings.TrimSpace(fbKey)
			if entry, exists := reg.cache[fbKey]; exists && entry.committed {
				return api.CacheRetrieveResp{
					Store:           reg.store,
					Key:             entry.key,
					Fallback:        true,
					StoreObjectName: entry.storeObjectName,
					ExpiresAt:       entry.expiresAt,
					CompressionType: entry.compression,
				}, true, nil
			}
		}
	}

	return api.CacheRetrieveResp{Message: api.CacheEntryNotFound}, false, nil
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
		toWrite := int64(chunkSize)
		if remaining < toWrite {
			toWrite = remaining
		}

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
	case "local_s3":
		bucketURL = fmt.Sprintf("file://%s", urlPath) // Use file:// for testing
	default:
		t.Fatalf("unsupported storage type: %s", storageType)
	}

	// Create cache client
	c := &client{
		api:          mockClient,
		bucketURL:    bucketURL,
		format:       "zip",
		branch:       "main",
		pipeline:     "test-pipeline",
		organization: "test-org",
		platform:     "linux/amd64",
		registry:     "~",
		caches: []configuration.Cache{
			{
				ID:           "test-cache",
				Key:          "v1-test-key",
				Paths:        []string{cacheDir},
				FallbackKeys: []string{"v1-fallback-key"},
			},
		},
		onProgress: nil,
	}

	return c, cacheDir, storageDir
}

func TestCacheIntegration_SaveAndRestore(t *testing.T) {
	ctx := context.Background()

	// Setup test cache with local file storage
	cacheClient, cacheDir, storageDir := setupTestCache(t, "local_file")

	// Save the cache
	t.Run("save", func(t *testing.T) {
		result, err := cacheClient.Save(ctx, "test-cache")
		if err != nil {
			t.Fatalf("Save: %v", err)
		}

		if !result.CacheCreated {
			t.Error("cache should be created")
		}
		if result.Key != "v1-test-key" {
			t.Errorf("Key = %q, want %q", result.Key, "v1-test-key")
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
		if result.Key != "v1-test-key" {
			t.Errorf("Key = %q, want %q", result.Key, "v1-test-key")
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
	ctx := context.Background()

	cacheClient, _, _ := setupTestCache(t, "local_file")

	// Save the cache first time
	result1, err := cacheClient.Save(ctx, "test-cache")
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !result1.CacheCreated {
		t.Error("first save should create cache")
	}

	// Try to save again
	result2, err := cacheClient.Save(ctx, "test-cache")
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if result2.CacheCreated {
		t.Error("second save should not create cache")
	}
	if result2.Transfer != nil {
		t.Error("should not have transfer info when cache exists")
	}
	if result2.Key != "v1-test-key" {
		t.Errorf("Key = %q, want %q", result2.Key, "v1-test-key")
	}
}

func TestCacheIntegration_RestoreCacheMiss(t *testing.T) {
	ctx := context.Background()

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
	if result.Key != "v1-test-key" {
		t.Errorf("should return requested key, Key = %q, want %q", result.Key, "v1-test-key")
	}
}

func TestCacheIntegration_RestoreWithFallback(t *testing.T) {
	ctx := context.Background()

	// Use setupTestCache to create test environment
	cacheClient, cacheDir, _ := setupTestCache(t, "local_file")

	// Save the fallback cache first
	cacheClient.caches[0].Key = "v1-fallback-key"
	cacheClient.caches[0].FallbackKeys = []string{}
	saveResult, err := cacheClient.Save(ctx, "test-cache")
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !saveResult.CacheCreated {
		t.Error("fallback cache should be created")
	}
	t.Logf("Saved fallback cache with key: %s", saveResult.Key)

	// Clean up cache directory
	if err := os.RemoveAll(cacheDir); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Now try to restore with a different key that has the fallback
	cacheClient.caches[0].Key = "v1-test-key"
	cacheClient.caches[0].FallbackKeys = []string{"v1-fallback-key"}
	result, err := cacheClient.Restore(ctx, "test-cache")
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}

	// Should use the fallback key
	if !result.CacheRestored {
		t.Error("cache should be restored")
	}
	if !result.FallbackUsed {
		t.Error("should use fallback key")
	}
	if result.CacheHit {
		t.Error("should not be exact hit")
	}
	if result.Key != "v1-fallback-key" {
		t.Errorf("should return fallback key, Key = %q, want %q", result.Key, "v1-fallback-key")
	}

	t.Logf("Restore result: restored=%v, hit=%v, fallback=%v, key=%s",
		result.CacheRestored, result.CacheHit, result.FallbackUsed, result.Key)

	// Verify files were restored
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) == 0 {
		t.Error("cache directory should have restored files")
	}
}

func TestCacheIntegration_LargeFileChecksum(t *testing.T) {
	ctx := context.Background()

	cacheClient, cacheDir, _ := setupTestCache(t, "local_file")

	// Save cache
	result, err := cacheClient.Save(ctx, "test-cache")
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !result.CacheCreated {
		t.Fatal("expected CacheCreated to be true")
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
	if result2.CacheCreated {
		t.Error("cache should already exist")
	}
}

func TestCacheIntegration_TransferMetrics(t *testing.T) {
	ctx := context.Background()

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
