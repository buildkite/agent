package zstash

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/buildkite/agent/v3/internal/zstash/api"
	"github.com/buildkite/agent/v3/internal/zstash/cache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))

	f, err := os.Create(path)
	require.NoError(t, err)
	defer f.Close()

	// Write random data in chunks to avoid memory issues with large files
	const chunkSize = 1024 * 1024 // 1MB chunks
	buf := make([]byte, chunkSize)
	remaining := sizeBytes

	for remaining > 0 {
		toWrite := int64(chunkSize)
		if remaining < toWrite {
			toWrite = remaining
		}

		_, err := rand.Read(buf[:toWrite])
		require.NoError(t, err)

		_, err = f.Write(buf[:toWrite])
		require.NoError(t, err)

		remaining -= toWrite
	}

	require.NoError(t, f.Sync())
}

// setupTestCache creates a test cache with temporary directories and files
func setupTestCache(t *testing.T, storageType string) (cacheClient *Cache, cacheDir, storageDir string) {
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

	require.NoError(t, os.MkdirAll(cacheDir, 0o755))
	require.NoError(t, os.MkdirAll(storageDir, 0o755))

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

	// Build storage URL based on type (need absolute path for file:// URLs)
	absStorageDir, err := filepath.Abs(storageDir)
	require.NoError(t, err)

	var bucketURL string
	switch storageType {
	case "local_file":
		bucketURL = fmt.Sprintf("file://%s", absStorageDir)
	case "local_s3":
		bucketURL = fmt.Sprintf("file://%s", absStorageDir) // Use file:// for testing
	default:
		t.Fatalf("unsupported storage type: %s", storageType)
	}

	// Create cache client
	client := &Cache{
		client:       mockClient,
		bucketURL:    bucketURL,
		format:       "zip",
		branch:       "main",
		pipeline:     "test-pipeline",
		organization: "test-org",
		platform:     "linux/amd64",
		registry:     "~",
		caches: []cache.Cache{
			{
				ID:           "test-cache",
				Key:          "v1-test-key",
				Paths:        []string{cacheDir},
				FallbackKeys: []string{"v1-fallback-key"},
			},
		},
		onProgress: nil,
	}

	return client, cacheDir, storageDir
}

func TestCacheIntegration_SaveAndRestore(t *testing.T) {
	ctx := context.Background()

	// Setup test cache with local file storage
	cacheClient, cacheDir, storageDir := setupTestCache(t, "local_file")

	// Save the cache
	t.Run("save", func(t *testing.T) {
		result, err := cacheClient.Save(ctx, "test-cache")
		require.NoError(t, err)

		assert.True(t, result.CacheCreated, "cache should be created")
		assert.Equal(t, "v1-test-key", result.Key)
		assert.NotEmpty(t, result.UploadID)
		assert.Greater(t, result.Archive.Size, int64(0), "archive should have size")
		assert.Greater(t, result.Archive.WrittenBytes, int64(0), "should have written bytes")
		assert.Greater(t, result.Archive.WrittenEntries, int64(0), "should have entries")
		assert.NotEmpty(t, result.Archive.Sha256Sum, "should have SHA256 checksum")
		require.NotNil(t, result.Transfer, "should have transfer info")
		assert.Greater(t, result.Transfer.BytesTransferred, int64(0), "should have transferred bytes")
		assert.Greater(t, result.TotalDuration, time.Duration(0), "should have duration")

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
		require.NoError(t, err)
		assert.NotEmpty(t, entries, "storage directory should contain files")
	})

	// Remove the original cache files to simulate a fresh checkout
	t.Run("cleanup_cache_dir", func(t *testing.T) {
		require.NoError(t, os.RemoveAll(cacheDir))
		require.NoError(t, os.MkdirAll(cacheDir, 0o755))

		// Verify directory is empty
		entries, err := os.ReadDir(cacheDir)
		require.NoError(t, err)
		assert.Empty(t, entries, "cache directory should be empty")
	})

	// Restore the cache
	t.Run("restore", func(t *testing.T) {
		result, err := cacheClient.Restore(ctx, "test-cache")
		require.NoError(t, err)

		assert.True(t, result.CacheRestored, "cache should be restored")
		assert.True(t, result.CacheHit, "should be exact key match")
		assert.False(t, result.FallbackUsed, "should not use fallback")
		assert.Equal(t, "v1-test-key", result.Key)
		assert.Greater(t, result.Archive.Size, int64(0), "archive should have size")
		assert.Greater(t, result.Archive.WrittenBytes, int64(0), "should have written bytes")
		assert.Greater(t, result.Archive.WrittenEntries, int64(0), "should have entries")
		assert.Greater(t, result.Transfer.BytesTransferred, int64(0), "should have transferred bytes")
		assert.Greater(t, result.TotalDuration, time.Duration(0), "should have duration")

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
			assert.FileExists(t, file, "restored file should exist: %s", file)

			// Verify file size is approximately correct (~33MB each)
			stat, err := os.Stat(file)
			require.NoError(t, err)
			assert.InDelta(t, 33*1024*1024, stat.Size(), 1024*1024, "file size should be ~33MB")
		}
	})
}

func TestCacheIntegration_SaveAlreadyExists(t *testing.T) {
	ctx := context.Background()

	cacheClient, _, _ := setupTestCache(t, "local_file")

	// Save the cache first time
	result1, err := cacheClient.Save(ctx, "test-cache")
	require.NoError(t, err)
	assert.True(t, result1.CacheCreated, "first save should create cache")

	// Try to save again
	result2, err := cacheClient.Save(ctx, "test-cache")
	require.NoError(t, err)
	assert.False(t, result2.CacheCreated, "second save should not create cache")
	assert.Nil(t, result2.Transfer, "should not have transfer info when cache exists")
	assert.Equal(t, "v1-test-key", result2.Key)
}

func TestCacheIntegration_RestoreCacheMiss(t *testing.T) {
	ctx := context.Background()

	cacheClient, _, _ := setupTestCache(t, "local_file")

	// Try to restore without saving first
	result, err := cacheClient.Restore(ctx, "test-cache")
	require.NoError(t, err)

	assert.False(t, result.CacheRestored, "should not restore when cache doesn't exist")
	assert.False(t, result.CacheHit, "should not be a cache hit")
	assert.False(t, result.FallbackUsed, "should not use fallback")
	assert.Equal(t, "v1-test-key", result.Key, "should return requested key")
}

func TestCacheIntegration_RestoreWithFallback(t *testing.T) {
	ctx := context.Background()

	// Use setupTestCache to create test environment
	cacheClient, cacheDir, _ := setupTestCache(t, "local_file")

	// Save the fallback cache first
	cacheClient.caches[0].Key = "v1-fallback-key"
	cacheClient.caches[0].FallbackKeys = []string{}
	saveResult, err := cacheClient.Save(ctx, "test-cache")
	require.NoError(t, err)
	assert.True(t, saveResult.CacheCreated, "fallback cache should be created")
	t.Logf("Saved fallback cache with key: %s", saveResult.Key)

	// Clean up cache directory
	require.NoError(t, os.RemoveAll(cacheDir))
	require.NoError(t, os.MkdirAll(cacheDir, 0o755))

	// Now try to restore with a different key that has the fallback
	cacheClient.caches[0].Key = "v1-test-key"
	cacheClient.caches[0].FallbackKeys = []string{"v1-fallback-key"}
	result, err := cacheClient.Restore(ctx, "test-cache")
	require.NoError(t, err)

	// Should use the fallback key
	assert.True(t, result.CacheRestored, "cache should be restored")
	assert.True(t, result.FallbackUsed, "should use fallback key")
	assert.False(t, result.CacheHit, "should not be exact hit")
	assert.Equal(t, "v1-fallback-key", result.Key, "should return fallback key")

	t.Logf("Restore result: restored=%v, hit=%v, fallback=%v, key=%s",
		result.CacheRestored, result.CacheHit, result.FallbackUsed, result.Key)

	// Verify files were restored
	entries, err := os.ReadDir(cacheDir)
	require.NoError(t, err)
	assert.NotEmpty(t, entries, "cache directory should have restored files")
}

func TestCacheIntegration_LargeFileChecksum(t *testing.T) {
	ctx := context.Background()

	cacheClient, cacheDir, _ := setupTestCache(t, "local_file")

	// Save cache
	result, err := cacheClient.Save(ctx, "test-cache")
	require.NoError(t, err)
	require.True(t, result.CacheCreated)

	checksum1 := result.Archive.Sha256Sum
	assert.NotEmpty(t, checksum1, "should have SHA256 checksum")

	// Restore and save again
	require.NoError(t, os.RemoveAll(cacheDir))
	require.NoError(t, os.MkdirAll(cacheDir, 0o755))

	_, err = cacheClient.Restore(ctx, "test-cache")
	require.NoError(t, err)

	// Save again - should be detected as already exists
	result2, err := cacheClient.Save(ctx, "test-cache")
	require.NoError(t, err)
	assert.False(t, result2.CacheCreated, "cache should already exist")
}

func TestCacheIntegration_TransferMetrics(t *testing.T) {
	ctx := context.Background()

	cacheClient, _, _ := setupTestCache(t, "local_file")

	// Save cache and check transfer metrics
	saveResult, err := cacheClient.Save(ctx, "test-cache")
	require.NoError(t, err)
	require.NotNil(t, saveResult.Transfer)

	assert.Greater(t, saveResult.Transfer.BytesTransferred, int64(0))
	assert.Greater(t, saveResult.Transfer.TransferSpeed, 0.0)
	assert.Greater(t, saveResult.Transfer.Duration, time.Duration(0))

	t.Logf("Upload: %d bytes at %.2f MB/s in %s",
		saveResult.Transfer.BytesTransferred,
		saveResult.Transfer.TransferSpeed,
		saveResult.Transfer.Duration)

	// Restore cache and check transfer metrics
	restoreResult, err := cacheClient.Restore(ctx, "test-cache")
	require.NoError(t, err)

	assert.Greater(t, restoreResult.Transfer.BytesTransferred, int64(0))
	assert.Greater(t, restoreResult.Transfer.TransferSpeed, 0.0)
	assert.Greater(t, restoreResult.Transfer.Duration, time.Duration(0))

	t.Logf("Download: %d bytes at %.2f MB/s in %s",
		restoreResult.Transfer.BytesTransferred,
		restoreResult.Transfer.TransferSpeed,
		restoreResult.Transfer.Duration)
}
