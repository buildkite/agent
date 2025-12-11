package cache

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/zstash"
	"github.com/buildkite/zstash/cache"
	"github.com/stretchr/testify/require"
)

// mockCacheClient is a mock implementation of the CacheClient interface for testing
type mockCacheClient struct {
	saveFunc    func(ctx context.Context, cacheID string) (zstash.SaveResult, error)
	restoreFunc func(ctx context.Context, cacheID string) (zstash.RestoreResult, error)
	listFunc    func() []cache.Cache
}

func (m *mockCacheClient) Save(ctx context.Context, cacheID string) (zstash.SaveResult, error) {
	if m.saveFunc != nil {
		return m.saveFunc(ctx, cacheID)
	}
	return zstash.SaveResult{}, nil
}

func (m *mockCacheClient) Restore(ctx context.Context, cacheID string) (zstash.RestoreResult, error) {
	if m.restoreFunc != nil {
		return m.restoreFunc(ctx, cacheID)
	}
	return zstash.RestoreResult{}, nil
}

func (m *mockCacheClient) ListCaches() []cache.Cache {
	if m.listFunc != nil {
		return m.listFunc()
	}
	return nil
}

// Test helpers

func createTempCacheConfig(t *testing.T, content string) string {
	t.Helper()
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "cache.yml")
	err := os.WriteFile(configFile, []byte(content), 0o600)
	require.NoError(t, err)
	return configFile
}

// Tests for saveWithClient

func TestSaveWithClient_CacheCreated(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	mock := &mockCacheClient{
		saveFunc: func(ctx context.Context, cacheID string) (zstash.SaveResult, error) {
			return zstash.SaveResult{
				CacheCreated: true,
				Key:          "test-key-v1",
				Archive: zstash.ArchiveMetrics{
					Size:             1024,
					WrittenBytes:     1024,
					WrittenEntries:   10,
					CompressionRatio: 2.5,
				},
				Transfer: &zstash.TransferMetrics{
					TransferSpeed: 5.5,
				},
			}, nil
		},
	}

	err := saveWithClient(ctx, logger.Discard, mock, []string{"cache1"}, 1)
	require.NoError(t, err)
}

func TestSaveWithClient_CacheAlreadyExists(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	mock := &mockCacheClient{
		saveFunc: func(ctx context.Context, cacheID string) (zstash.SaveResult, error) {
			return zstash.SaveResult{
				CacheCreated: false,
				Key:          "test-key-v1",
			}, nil
		},
	}

	err := saveWithClient(ctx, logger.Discard, mock, []string{"cache1"}, 1)
	require.NoError(t, err)
}

func TestSaveWithClient_MultipleCaches(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	callCount := 0
	mock := &mockCacheClient{
		saveFunc: func(ctx context.Context, cacheID string) (zstash.SaveResult, error) {
			callCount++
			return zstash.SaveResult{
				CacheCreated: true,
				Key:          fmt.Sprintf("key-%s", cacheID),
				Archive: zstash.ArchiveMetrics{
					Size:             100,
					WrittenBytes:     100,
					WrittenEntries:   1,
					CompressionRatio: 1.0,
				},
				Transfer: &zstash.TransferMetrics{
					TransferSpeed: 1.0,
				},
			}, nil
		},
	}

	err := saveWithClient(ctx, logger.Discard, mock, []string{"cache1", "cache2", "cache3"}, 1)
	require.NoError(t, err)
	require.Equal(t, 3, callCount, "Expected Save to be called 3 times")
}

func TestSaveWithClient_Error(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	expectedErr := errors.New("save failed")
	mock := &mockCacheClient{
		saveFunc: func(ctx context.Context, cacheID string) (zstash.SaveResult, error) {
			return zstash.SaveResult{}, expectedErr
		},
	}

	err := saveWithClient(ctx, logger.Discard, mock, []string{"cache1"}, 1)
	require.Error(t, err)
	require.ErrorContains(t, err, "failed to save cache")
	require.ErrorContains(t, err, "save failed")
}

func TestSaveWithClient_EmptyCacheIDs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	mock := &mockCacheClient{
		saveFunc: func(ctx context.Context, cacheID string) (zstash.SaveResult, error) {
			t.Fatal("Save should not be called with empty cache IDs")
			return zstash.SaveResult{}, nil
		},
	}

	err := saveWithClient(ctx, logger.Discard, mock, []string{}, 1)
	require.NoError(t, err)
}

// Tests for restoreWithClient

func TestRestoreWithClient_CacheHit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	mock := &mockCacheClient{
		restoreFunc: func(ctx context.Context, cacheID string) (zstash.RestoreResult, error) {
			return zstash.RestoreResult{
				CacheHit:      true,
				CacheRestored: true,
				FallbackUsed:  false,
				Key:           "test-key-v1",
				Archive: zstash.ArchiveMetrics{
					Size:             1024,
					WrittenBytes:     1024,
					WrittenEntries:   10,
					CompressionRatio: 2.5,
				},
				Transfer: zstash.TransferMetrics{
					TransferSpeed: 5.5,
				},
			}, nil
		},
	}

	err := restoreWithClient(ctx, logger.Discard, mock, []string{"cache1"}, 1)
	require.NoError(t, err)
}

func TestRestoreWithClient_FallbackUsed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	mock := &mockCacheClient{
		restoreFunc: func(ctx context.Context, cacheID string) (zstash.RestoreResult, error) {
			return zstash.RestoreResult{
				CacheHit:      false,
				CacheRestored: true,
				FallbackUsed:  true,
				Key:           "test-key-fallback",
				Archive: zstash.ArchiveMetrics{
					Size:             512,
					WrittenBytes:     512,
					WrittenEntries:   5,
					CompressionRatio: 2.0,
				},
				Transfer: zstash.TransferMetrics{
					TransferSpeed: 3.5,
				},
			}, nil
		},
	}

	err := restoreWithClient(ctx, logger.Discard, mock, []string{"cache1"}, 1)
	require.NoError(t, err)
}

func TestRestoreWithClient_CacheMiss(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	mock := &mockCacheClient{
		restoreFunc: func(ctx context.Context, cacheID string) (zstash.RestoreResult, error) {
			return zstash.RestoreResult{
				CacheHit:      false,
				CacheRestored: false,
				FallbackUsed:  false,
				Key:           "test-key-v1",
			}, nil
		},
	}

	err := restoreWithClient(ctx, logger.Discard, mock, []string{"cache1"}, 1)
	require.NoError(t, err)
}

func TestRestoreWithClient_MultipleCaches(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	callCount := 0
	mock := &mockCacheClient{
		restoreFunc: func(ctx context.Context, cacheID string) (zstash.RestoreResult, error) {
			callCount++
			return zstash.RestoreResult{
				CacheHit:      true,
				CacheRestored: true,
				Key:           fmt.Sprintf("key-%s", cacheID),
			}, nil
		},
	}

	err := restoreWithClient(ctx, logger.Discard, mock, []string{"cache1", "cache2", "cache3"}, 1)
	require.NoError(t, err)
	require.Equal(t, 3, callCount, "Expected Restore to be called 3 times")
}

func TestRestoreWithClient_Error(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	expectedErr := errors.New("restore failed")
	mock := &mockCacheClient{
		restoreFunc: func(ctx context.Context, cacheID string) (zstash.RestoreResult, error) {
			return zstash.RestoreResult{}, expectedErr
		},
	}

	err := restoreWithClient(ctx, logger.Discard, mock, []string{"cache1"}, 1)
	require.Error(t, err)
	require.ErrorContains(t, err, "failed to restore cache")
	require.ErrorContains(t, err, "restore failed")
}

func TestRestoreWithClient_EmptyCacheIDs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	mock := &mockCacheClient{
		restoreFunc: func(ctx context.Context, cacheID string) (zstash.RestoreResult, error) {
			t.Fatal("Restore should not be called with empty cache IDs")
			return zstash.RestoreResult{}, nil
		},
	}

	err := restoreWithClient(ctx, logger.Discard, mock, []string{}, 1)
	require.NoError(t, err)
}

// Tests for loadCacheConfiguration

func TestLoadCacheConfiguration_Valid(t *testing.T) {
	t.Parallel()

	config := `dependencies:
  - id: node
    key: 'node-{{ checksum "package-lock.json" }}'
    paths:
      - node_modules
  - id: ruby
    key: 'ruby-{{ checksum "Gemfile.lock" }}'
    paths:
      - vendor/bundle
`
	configFile := createTempCacheConfig(t, config)

	fileConfig, err := loadCacheConfiguration(configFile)
	require.NoError(t, err)
	require.Len(t, fileConfig.Dependencies, 2)
	require.Equal(t, "node", fileConfig.Dependencies[0].ID)
	require.Equal(t, "ruby", fileConfig.Dependencies[1].ID)
}

func TestLoadCacheConfiguration_InvalidYAML(t *testing.T) {
	t.Parallel()

	config := `dependencies:
  - id: node
    key: test
    paths
      - invalid indentation here
    : wrong syntax
`
	configFile := createTempCacheConfig(t, config)

	_, err := loadCacheConfiguration(configFile)
	require.Error(t, err)
	require.ErrorContains(t, err, "failed to unmarshal cache config file")
}

func TestLoadCacheConfiguration_FileNotFound(t *testing.T) {
	t.Parallel()

	_, err := loadCacheConfiguration("/nonexistent/path/to/cache.yml")
	require.Error(t, err)
	require.ErrorContains(t, err, "failed to read cache config file")
}

func TestLoadCacheConfiguration_EmptyFile(t *testing.T) {
	t.Parallel()

	configFile := createTempCacheConfig(t, "")

	fileConfig, err := loadCacheConfiguration(configFile)
	require.NoError(t, err)
	require.Empty(t, fileConfig.Dependencies)
}

// Tests for setupCacheClient

func TestSetupCacheClient_InvalidCacheIDs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	config := `dependencies:
  - id: cache1
    key: 'test-key-1'
    paths:
      - path1
  - id: cache2
    key: 'test-key-2'
    paths:
      - path2
`
	configFile := createTempCacheConfig(t, config)

	cfg := Config{
		CacheConfigFile: configFile,
		Ids:             []string{"cache1", "invalid1", "cache2", "invalid2"},
		BucketURL:       "s3://test-bucket",
		Branch:          "main",
		Pipeline:        "test-pipeline",
		Organization:    "test-org",
		APIEndpoint:     "https://api.buildkite.com/v3",
		APIToken:        "test-token",
	}

	_, _, err := setupCacheClient(ctx, logger.Discard, cfg)
	require.Error(t, err)
	require.ErrorContains(t, err, "cache IDs not found in configuration")
	require.ErrorContains(t, err, "invalid1")
	require.ErrorContains(t, err, "invalid2")
}

func TestSetupCacheClient_ValidCacheIDs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	config := `dependencies:
  - id: cache1
    key: 'test-key-1'
    paths:
      - path1
  - id: cache2
    key: 'test-key-2'
    paths:
      - path2
`
	configFile := createTempCacheConfig(t, config)

	cfg := Config{
		CacheConfigFile: configFile,
		Ids:             []string{"cache1", "cache2"},
		BucketURL:       "s3://test-bucket",
		Branch:          "main",
		Pipeline:        "test-pipeline",
		Organization:    "test-org",
		APIEndpoint:     "https://api.buildkite.com/v3",
		APIToken:        "test-token",
	}

	client, cacheIDs, err := setupCacheClient(ctx, logger.Discard, cfg)
	require.NoError(t, err)
	require.NotNil(t, client)
	require.Equal(t, []string{"cache1", "cache2"}, cacheIDs)
}

func TestSetupCacheClient_AllCaches(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	config := `dependencies:
  - id: cache1
    key: 'test-key-1'
    paths:
      - path1
  - id: cache2
    key: 'test-key-2'
    paths:
      - path2
`
	configFile := createTempCacheConfig(t, config)

	cfg := Config{
		CacheConfigFile: configFile,
		Ids:             []string{},
		BucketURL:       "s3://test-bucket",
		Branch:          "main",
		Pipeline:        "test-pipeline",
		Organization:    "test-org",
		APIEndpoint:     "https://api.buildkite.com/v3",
		APIToken:        "test-token",
	}

	client, cacheIDs, err := setupCacheClient(ctx, logger.Discard, cfg)
	require.NoError(t, err)
	require.NotNil(t, client)
	require.ElementsMatch(t, []string{"cache1", "cache2"}, cacheIDs)
}
