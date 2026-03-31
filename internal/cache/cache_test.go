package cache

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/buildkite/agent/v4/logger"
	"github.com/buildkite/zstash"
	"github.com/buildkite/zstash/cache"
	"github.com/google/go-cmp/cmp"
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
	if err != nil {
		t.Fatalf("os.WriteFile(%q, []byte(content), %d) error = %v, want nil", configFile, 0o600, err)
	}
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
	if err != nil {
		t.Fatalf("saveWithClient(ctx, logger.Discard, mock, []string{\"cache1\"}, %d) error = %v, want nil", 1, err)
	}
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
	if err != nil {
		t.Fatalf("saveWithClient(ctx, logger.Discard, mock, []string{\"cache1\"}, %d) error = %v, want nil", 1, err)
	}
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
	if err != nil {
		t.Fatalf("saveWithClient(ctx, logger.Discard, mock, []string{\"cache1\", \"cache2\", \"cache3\"}, %d) error = %v, want nil", 1, err)
	}
	if got, want := callCount, 3; got != want {
		t.Fatalf("Expected Save to be called 3 times")
	}
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
	if err == nil {
		t.Fatalf("saveWithClient(ctx, logger.Discard, mock, []string{\"cache1\"}, %d) error = %v, want non-nil error", 1, err)
	}
	if want := "failed to save cache"; !strings.Contains(err.Error(), want) {
		t.Fatalf("saveWithClient(ctx, logger.Discard, mock, []string{\"cache1\"}, %d) error = %v, want error containing %q", 1, err, want)
	}
	if want := "save failed"; !strings.Contains(err.Error(), want) {
		t.Fatalf("saveWithClient(ctx, logger.Discard, mock, []string{\"cache1\"}, %d) error = %v, want error containing %q", 1, err, want)
	}
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
	if err != nil {
		t.Fatalf("saveWithClient(ctx, logger.Discard, mock, []string{}, %d) error = %v, want nil", 1, err)
	}
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
	if err != nil {
		t.Fatalf("restoreWithClient(ctx, logger.Discard, mock, []string{\"cache1\"}, %d) error = %v, want nil", 1, err)
	}
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
	if err != nil {
		t.Fatalf("restoreWithClient(ctx, logger.Discard, mock, []string{\"cache1\"}, %d) error = %v, want nil", 1, err)
	}
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
	if err != nil {
		t.Fatalf("restoreWithClient(ctx, logger.Discard, mock, []string{\"cache1\"}, %d) error = %v, want nil", 1, err)
	}
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
	if err != nil {
		t.Fatalf("restoreWithClient(ctx, logger.Discard, mock, []string{\"cache1\", \"cache2\", \"cache3\"}, %d) error = %v, want nil", 1, err)
	}
	if got, want := callCount, 3; got != want {
		t.Fatalf("Expected Restore to be called 3 times")
	}
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
	if err == nil {
		t.Fatalf("restoreWithClient(ctx, logger.Discard, mock, []string{\"cache1\"}, %d) error = %v, want non-nil error", 1, err)
	}
	if want := "failed to restore cache"; !strings.Contains(err.Error(), want) {
		t.Fatalf("restoreWithClient(ctx, logger.Discard, mock, []string{\"cache1\"}, %d) error = %v, want error containing %q", 1, err, want)
	}
	if want := "restore failed"; !strings.Contains(err.Error(), want) {
		t.Fatalf("restoreWithClient(ctx, logger.Discard, mock, []string{\"cache1\"}, %d) error = %v, want error containing %q", 1, err, want)
	}
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
	if err != nil {
		t.Fatalf("restoreWithClient(ctx, logger.Discard, mock, []string{}, %d) error = %v, want nil", 1, err)
	}
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
	if err != nil {
		t.Fatalf("loadCacheConfiguration(%q) error = %v, want nil", configFile, err)
	}
	if got, want := len(fileConfig.Dependencies), 2; got != want {
		t.Fatalf("len(fileConfig.Dependencies) = %d, want %d", got, want)
	}
	if got, want := fileConfig.Dependencies[0].ID, "node"; got != want {
		t.Fatalf("fileConfig.Dependencies[0].ID = %q, want %q", got, want)
	}
	if got, want := fileConfig.Dependencies[1].ID, "ruby"; got != want {
		t.Fatalf("fileConfig.Dependencies[1].ID = %q, want %q", got, want)
	}
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
	if err == nil {
		t.Fatalf("loadCacheConfiguration(%q) error = %v, want non-nil error", configFile, err)
	}
	if want := "failed to unmarshal cache config file"; !strings.Contains(err.Error(), want) {
		t.Fatalf("loadCacheConfiguration(%q) error = %v, want error containing %q", configFile, err, want)
	}
}

func TestLoadCacheConfiguration_FileNotFound(t *testing.T) {
	t.Parallel()

	_, err := loadCacheConfiguration("/nonexistent/path/to/cache.yml")
	if err == nil {
		t.Fatalf("loadCacheConfiguration(%q) error = %v, want non-nil error", "/nonexistent/path/to/cache.yml", err)
	}
	if want := "failed to read cache config file"; !strings.Contains(err.Error(), want) {
		t.Fatalf("loadCacheConfiguration(%q) error = %v, want error containing %q", "/nonexistent/path/to/cache.yml", err, want)
	}
}

func TestLoadCacheConfiguration_EmptyFile(t *testing.T) {
	t.Parallel()

	configFile := createTempCacheConfig(t, "")

	fileConfig, err := loadCacheConfiguration(configFile)
	if err != nil {
		t.Fatalf("loadCacheConfiguration(%q) error = %v, want nil", configFile, err)
	}
	if got := len(fileConfig.Dependencies); got != 0 {
		t.Fatalf("len(fileConfig.Dependencies) = %d, want 0", got)
	}
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
	if err == nil {
		t.Fatalf("setupCacheClient(ctx, logger.Discard, cfg) error = %v, want non-nil error", err)
	}
	if want := "cache IDs not found in configuration"; !strings.Contains(err.Error(), want) {
		t.Fatalf("setupCacheClient(ctx, logger.Discard, cfg) error = %v, want error containing %q", err, want)
	}
	if want := "invalid1"; !strings.Contains(err.Error(), want) {
		t.Fatalf("setupCacheClient(ctx, logger.Discard, cfg) error = %v, want error containing %q", err, want)
	}
	if want := "invalid2"; !strings.Contains(err.Error(), want) {
		t.Fatalf("setupCacheClient(ctx, logger.Discard, cfg) error = %v, want error containing %q", err, want)
	}
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
	if err != nil {
		t.Fatalf("setupCacheClient(ctx, logger.Discard, cfg) error = %v, want nil", err)
	}
	if got := client; got == nil {
		t.Fatalf("setupCacheClient(ctx, logger.Discard, cfg) = %v, want non-nil value", got)
	}
	if diff := cmp.Diff(cacheIDs, []string{"cache1", "cache2"}); diff != "" {
		t.Fatalf("setupCacheClient(ctx, logger.Discard, cfg) diff (-got +want):\n%s", diff)
	}
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
	if err != nil {
		t.Fatalf("setupCacheClient(ctx, logger.Discard, cfg) error = %v, want nil", err)
	}
	if got := client; got == nil {
		t.Fatalf("setupCacheClient(ctx, logger.Discard, cfg) = %v, want non-nil value", got)
	}
	if diff := cmp.Diff(slices.Sorted(slices.Values(cacheIDs)), slices.Sorted(slices.Values([]string{"cache1", "cache2"}))); diff != "" {
		t.Fatalf("setupCacheClient(ctx, logger.Discard, cfg) sorted diff (-got +want):\n%s", diff)
	}
}
