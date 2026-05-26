package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewLocalFileBlob(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		url         string
		wantErr     bool
		errContains string
	}{
		{
			name:    "valid file URL with absolute path",
			url:     "file:///tmp/test-cache",
			wantErr: false,
		},

		{
			name:        "invalid scheme",
			url:         "http://localhost/cache",
			wantErr:     true,
			errContains: "must be file",
		},
		{
			name:        "empty path",
			url:         "file://",
			wantErr:     true,
			errContains: "cannot be empty",
		},
		{
			name:        "invalid URL",
			url:         "not a url",
			wantErr:     true,
			errContains: "must be file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blob, err := NewLocalFileBlob(ctx, tt.url)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
				assert.NotNil(t, blob)
				assert.NotEmpty(t, blob.root)
				assert.DirExists(t, blob.root)
			}
		})
	}
}

func TestValidateFileKey(t *testing.T) {
	tests := []struct {
		name        string
		key         string
		wantErr     bool
		errContains string
	}{
		{
			name:    "valid simple key",
			key:     "cache/linux/test.tar.gz",
			wantErr: false,
		},
		{
			name:    "valid key with dots and dashes",
			key:     "cache/my-project/v1.2.3/artifact.tar.gz",
			wantErr: false,
		},
		{
			name:        "empty key",
			key:         "",
			wantErr:     true,
			errContains: "cannot be empty",
		},
		{
			name:        "key with path traversal",
			key:         "cache/../etc/passwd",
			wantErr:     true,
			errContains: "dangerous pattern",
		},
		{
			name:        "key with double slash",
			key:         "cache//test",
			wantErr:     true,
			errContains: "dangerous pattern",
		},
		{
			name:        "key with invalid characters",
			key:         "cache/test;rm -rf /",
			wantErr:     true,
			errContains: "invalid characters",
		},

		{
			name:        "key with backticks",
			key:         "cache/`whoami`",
			wantErr:     true,
			errContains: "invalid characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateFileKey(tt.key)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestLocalFileBlobUpload(t *testing.T) {
	ctx := context.Background()

	tmpDir := t.TempDir()
	rootDir := filepath.Join(tmpDir, "cache-root")
	srcDir := filepath.Join(tmpDir, "source")

	require.NoError(t, os.MkdirAll(srcDir, 0o755))

	blob, err := NewLocalFileBlob(ctx, "file://"+rootDir)
	require.NoError(t, err)

	srcFile := filepath.Join(srcDir, "test.txt")
	testContent := []byte("Hello, World! This is a test file.")
	require.NoError(t, os.WriteFile(srcFile, testContent, 0o600))

	key := "test/cache/artifact.txt"

	info, err := blob.Upload(ctx, srcFile, key)
	require.NoError(t, err)
	assert.Equal(t, int64(len(testContent)), info.BytesTransferred)
	assert.Greater(t, info.TransferSpeed, 0.0)
	assert.NotZero(t, info.Duration)

	dataPath := filepath.Join(rootDir, "test/cache/artifact.txt")
	assert.FileExists(t, dataPath)

	metaPath := dataPath + ".attrs.json"
	assert.FileExists(t, metaPath)

	content, err := os.ReadFile(dataPath)
	require.NoError(t, err)
	assert.Equal(t, testContent, content)
}

func TestLocalFileBlobDownload(t *testing.T) {
	ctx := context.Background()

	tmpDir := t.TempDir()
	rootDir := filepath.Join(tmpDir, "cache-root")
	srcDir := filepath.Join(tmpDir, "source")
	destDir := filepath.Join(tmpDir, "dest")

	require.NoError(t, os.MkdirAll(srcDir, 0o755))
	require.NoError(t, os.MkdirAll(destDir, 0o755))

	blob, err := NewLocalFileBlob(ctx, "file://"+rootDir)
	require.NoError(t, err)

	srcFile := filepath.Join(srcDir, "test.txt")
	testContent := []byte("Hello, World! This is a test file.")
	require.NoError(t, os.WriteFile(srcFile, testContent, 0o600))

	key := "test/cache/artifact.txt"

	// Upload first
	_, err = blob.Upload(ctx, srcFile, key)
	require.NoError(t, err)

	// Then download
	destFile := filepath.Join(destDir, "downloaded.txt")
	info, err := blob.Download(ctx, key, destFile)
	require.NoError(t, err)
	assert.Equal(t, int64(len(testContent)), info.BytesTransferred)
	assert.Greater(t, info.TransferSpeed, 0.0)
	assert.NotZero(t, info.Duration)

	content, err := os.ReadFile(destFile)
	require.NoError(t, err)
	assert.Equal(t, testContent, content)
}

func TestLocalFileBlobUploadOverwrite(t *testing.T) {
	ctx := context.Background()

	tmpDir := t.TempDir()
	rootDir := filepath.Join(tmpDir, "cache-root")
	srcDir := filepath.Join(tmpDir, "source")

	require.NoError(t, os.MkdirAll(srcDir, 0o755))

	blob, err := NewLocalFileBlob(ctx, "file://"+rootDir)
	require.NoError(t, err)

	key := "test/cache/artifact.txt"

	// Upload original content
	srcFile1 := filepath.Join(srcDir, "test1.txt")
	content1 := []byte("Original content")
	require.NoError(t, os.WriteFile(srcFile1, content1, 0o600))
	_, err = blob.Upload(ctx, srcFile1, key)
	require.NoError(t, err)

	// Overwrite with new content
	srcFile2 := filepath.Join(srcDir, "test2.txt")
	content2 := []byte("Updated content")
	require.NoError(t, os.WriteFile(srcFile2, content2, 0o600))

	info, err := blob.Upload(ctx, srcFile2, key)
	require.NoError(t, err)
	assert.Equal(t, int64(len(content2)), info.BytesTransferred)

	// Verify the new content
	dataPath := filepath.Join(rootDir, "test/cache/artifact.txt")
	storedContent, err := os.ReadFile(dataPath)
	require.NoError(t, err)
	assert.Equal(t, content2, storedContent)
}

func TestLocalFileBlobDownloadNonExistent(t *testing.T) {
	ctx := context.Background()

	tmpDir := t.TempDir()
	rootDir := filepath.Join(tmpDir, "cache-root")
	destDir := filepath.Join(tmpDir, "dest")

	require.NoError(t, os.MkdirAll(destDir, 0o755))

	blob, err := NewLocalFileBlob(ctx, "file://"+rootDir)
	require.NoError(t, err)

	destFile := filepath.Join(destDir, "nonexistent.txt")
	_, err = blob.Download(ctx, "nonexistent/key", destFile)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to open source file")
}

func TestLocalFileBlobUploadInvalidKey(t *testing.T) {
	ctx := context.Background()

	tmpDir := t.TempDir()
	rootDir := filepath.Join(tmpDir, "cache-root")
	srcDir := filepath.Join(tmpDir, "source")

	require.NoError(t, os.MkdirAll(srcDir, 0o755))

	blob, err := NewLocalFileBlob(ctx, "file://"+rootDir)
	require.NoError(t, err)

	srcFile := filepath.Join(srcDir, "test.txt")
	require.NoError(t, os.WriteFile(srcFile, []byte("test"), 0o600))

	_, err = blob.Upload(ctx, srcFile, "../../../etc/passwd")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dangerous pattern")
}

func TestLocalFileBlobDownloadInvalidKey(t *testing.T) {
	ctx := context.Background()

	tmpDir := t.TempDir()
	rootDir := filepath.Join(tmpDir, "cache-root")
	destDir := filepath.Join(tmpDir, "dest")

	require.NoError(t, os.MkdirAll(destDir, 0o755))

	blob, err := NewLocalFileBlob(ctx, "file://"+rootDir)
	require.NoError(t, err)

	destFile := filepath.Join(destDir, "test.txt")
	_, err = blob.Download(ctx, "cache//invalid", destFile)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dangerous pattern")
}

func TestKeyToPaths(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	blob, err := NewLocalFileBlob(ctx, "file://"+tmpDir)
	require.NoError(t, err)

	tests := []struct {
		name        string
		key         string
		wantData    string
		wantMeta    string
		wantErr     bool
		errContains string
	}{
		{
			name:     "simple key",
			key:      "test.txt",
			wantData: filepath.Join(tmpDir, "test.txt"),
			wantMeta: filepath.Join(tmpDir, "test.txt.attrs.json"),
			wantErr:  false,
		},
		{
			name:     "nested key",
			key:      "cache/linux/artifact.tar.gz",
			wantData: filepath.Join(tmpDir, "cache/linux/artifact.tar.gz"),
			wantMeta: filepath.Join(tmpDir, "cache/linux/artifact.tar.gz.attrs.json"),
			wantErr:  false,
		},
		{
			name:     "key with leading slash",
			key:      "/cache/test.txt",
			wantData: filepath.Join(tmpDir, "cache/test.txt"),
			wantMeta: filepath.Join(tmpDir, "cache/test.txt.attrs.json"),
			wantErr:  false,
		},
		{
			name:        "key with traversal",
			key:         "../outside",
			wantErr:     true,
			errContains: "dangerous pattern",
		},
		{
			name:        "empty key after normalization",
			key:         ".",
			wantErr:     true,
			errContains: "invalid key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dataPath, metaPath, err := blob.keyToPaths(tt.key)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantData, dataPath)
				assert.Equal(t, tt.wantMeta, metaPath)
			}
		})
	}
}

func TestLocalFileBlobConcurrentUpload(t *testing.T) {
	ctx := context.Background()

	tmpDir := t.TempDir()
	rootDir := filepath.Join(tmpDir, "cache-root")
	srcDir := filepath.Join(tmpDir, "source")

	require.NoError(t, os.MkdirAll(srcDir, 0o755))

	blob, err := NewLocalFileBlob(ctx, "file://"+rootDir)
	require.NoError(t, err)

	key := "test/cache/concurrent.txt"

	// Create two source files with different content
	srcFile1 := filepath.Join(srcDir, "file1.txt")
	content1 := []byte("Content from goroutine 1")
	require.NoError(t, os.WriteFile(srcFile1, content1, 0o600))

	srcFile2 := filepath.Join(srcDir, "file2.txt")
	content2 := []byte("Content from goroutine 2")
	require.NoError(t, os.WriteFile(srcFile2, content2, 0o600))

	// Upload concurrently
	done := make(chan error, 2)

	go func() {
		_, err := blob.Upload(ctx, srcFile1, key)
		done <- err
	}()

	go func() {
		_, err := blob.Upload(ctx, srcFile2, key)
		done <- err
	}()

	// Wait for both uploads to complete
	err1 := <-done
	err2 := <-done

	// Both uploads should succeed (last-writer-wins)
	require.NoError(t, err1)
	require.NoError(t, err2)

	// Verify file exists and contains one of the two contents
	dataPath := filepath.Join(rootDir, "test/cache/concurrent.txt")
	assert.FileExists(t, dataPath)

	finalContent, err := os.ReadFile(dataPath)
	require.NoError(t, err)

	// Should be one of the two contents (last writer wins)
	validContent := string(finalContent) == string(content1) || string(finalContent) == string(content2)
	assert.True(t, validContent, "final content should be from one of the uploaders")
}

func TestNewBlobStoreLocalFile(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	blob, err := NewBlobStore(ctx, LocalFileStore, "file://"+tmpDir)
	require.NoError(t, err)
	assert.NotNil(t, blob)

	_, ok := blob.(*LocalFileBlob)
	assert.True(t, ok, "expected LocalFileBlob type")
}
