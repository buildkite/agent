package store

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNewLocalFileBlob(t *testing.T) {
	ctx := t.Context()

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
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.errContains)
				}
			} else {
				if err != nil {
					t.Fatalf("NewLocalFileBlob: %v", err)
				}
				if blob == nil {
					t.Fatal("expected non-nil blob")
				}
				if blob.root == "" {
					t.Error("expected non-empty blob.root")
				}
				info, statErr := os.Stat(blob.root)
				if statErr != nil {
					t.Errorf("expected blob.root %q to exist: %v", blob.root, statErr)
				} else if !info.IsDir() {
					t.Errorf("expected blob.root %q to be a directory", blob.root)
				}
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
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.errContains)
				}
			} else {
				if err != nil {
					t.Fatalf("validateFileKey: %v", err)
				}
			}
		})
	}
}

func TestLocalFileBlobUpload(t *testing.T) {
	ctx := t.Context()

	tmpDir := t.TempDir()
	rootDir := filepath.Join(tmpDir, "cache-root")
	srcDir := filepath.Join(tmpDir, "source")

	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	blob, err := NewLocalFileBlob(ctx, fileURL(rootDir))
	if err != nil {
		t.Fatalf("NewLocalFileBlob: %v", err)
	}

	srcFile := filepath.Join(srcDir, "test.txt")
	testContent := []byte("Hello, World! This is a test file.")
	if err := os.WriteFile(srcFile, testContent, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	key := "test/cache/artifact.txt"

	info, err := blob.Upload(ctx, srcFile, key)
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if info.BytesTransferred != int64(len(testContent)) {
		t.Errorf("BytesTransferred: got %d, want %d", info.BytesTransferred, len(testContent))
	}
	if info.TransferSpeed <= 0.0 {
		t.Errorf("expected TransferSpeed > 0, got %f", info.TransferSpeed)
	}
	if info.Duration < 0 {
		t.Errorf("expected non-negative Duration, got %v", info.Duration)
	}

	dataPath := filepath.Join(rootDir, "test/cache/artifact.txt")
	if _, err := os.Stat(dataPath); err != nil {
		t.Errorf("expected file %q to exist: %v", dataPath, err)
	}

	metaPath := dataPath + ".attrs.json"
	if _, err := os.Stat(metaPath); err != nil {
		t.Errorf("expected file %q to exist: %v", metaPath, err)
	}

	content, err := os.ReadFile(dataPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(content, testContent) {
		t.Errorf("content mismatch: got %q, want %q", content, testContent)
	}
}

func TestLocalFileBlobDownload(t *testing.T) {
	ctx := t.Context()

	tmpDir := t.TempDir()
	rootDir := filepath.Join(tmpDir, "cache-root")
	srcDir := filepath.Join(tmpDir, "source")
	destDir := filepath.Join(tmpDir, "dest")

	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("MkdirAll srcDir: %v", err)
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatalf("MkdirAll destDir: %v", err)
	}

	blob, err := NewLocalFileBlob(ctx, fileURL(rootDir))
	if err != nil {
		t.Fatalf("NewLocalFileBlob: %v", err)
	}

	srcFile := filepath.Join(srcDir, "test.txt")
	testContent := []byte("Hello, World! This is a test file.")
	if err := os.WriteFile(srcFile, testContent, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	key := "test/cache/artifact.txt"

	// Upload first
	_, err = blob.Upload(ctx, srcFile, key)
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}

	// Then download
	destFile := filepath.Join(destDir, "downloaded.txt")
	info, err := blob.Download(ctx, key, destFile)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if info.BytesTransferred != int64(len(testContent)) {
		t.Errorf("BytesTransferred: got %d, want %d", info.BytesTransferred, len(testContent))
	}
	if info.TransferSpeed <= 0.0 {
		t.Errorf("expected TransferSpeed > 0, got %f", info.TransferSpeed)
	}
	if info.Duration < 0 {
		t.Errorf("expected non-negative Duration, got %v", info.Duration)
	}

	content, err := os.ReadFile(destFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(content, testContent) {
		t.Errorf("content mismatch: got %q, want %q", content, testContent)
	}
}

func TestLocalFileBlobUploadOverwrite(t *testing.T) {
	ctx := t.Context()

	tmpDir := t.TempDir()
	rootDir := filepath.Join(tmpDir, "cache-root")
	srcDir := filepath.Join(tmpDir, "source")

	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	blob, err := NewLocalFileBlob(ctx, fileURL(rootDir))
	if err != nil {
		t.Fatalf("NewLocalFileBlob: %v", err)
	}

	key := "test/cache/artifact.txt"

	// Upload original content
	srcFile1 := filepath.Join(srcDir, "test1.txt")
	content1 := []byte("Original content")
	if err := os.WriteFile(srcFile1, content1, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err = blob.Upload(ctx, srcFile1, key)
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}

	// Overwrite with new content
	srcFile2 := filepath.Join(srcDir, "test2.txt")
	content2 := []byte("Updated content")
	if err := os.WriteFile(srcFile2, content2, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	info, err := blob.Upload(ctx, srcFile2, key)
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if info.BytesTransferred != int64(len(content2)) {
		t.Errorf("BytesTransferred: got %d, want %d", info.BytesTransferred, len(content2))
	}

	// Verify the new content
	dataPath := filepath.Join(rootDir, "test/cache/artifact.txt")
	storedContent, err := os.ReadFile(dataPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(storedContent, content2) {
		t.Errorf("content mismatch: got %q, want %q", storedContent, content2)
	}
}

func TestLocalFileBlobDownloadNonExistent(t *testing.T) {
	ctx := t.Context()

	tmpDir := t.TempDir()
	rootDir := filepath.Join(tmpDir, "cache-root")
	destDir := filepath.Join(tmpDir, "dest")

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	blob, err := NewLocalFileBlob(ctx, fileURL(rootDir))
	if err != nil {
		t.Fatalf("NewLocalFileBlob: %v", err)
	}

	destFile := filepath.Join(destDir, "nonexistent.txt")
	_, err = blob.Download(ctx, "nonexistent/key", destFile)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to open source file") {
		t.Errorf("error %q does not contain %q", err.Error(), "failed to open source file")
	}
}

func TestLocalFileBlobUploadInvalidKey(t *testing.T) {
	ctx := t.Context()

	tmpDir := t.TempDir()
	rootDir := filepath.Join(tmpDir, "cache-root")
	srcDir := filepath.Join(tmpDir, "source")

	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	blob, err := NewLocalFileBlob(ctx, fileURL(rootDir))
	if err != nil {
		t.Fatalf("NewLocalFileBlob: %v", err)
	}

	srcFile := filepath.Join(srcDir, "test.txt")
	if err := os.WriteFile(srcFile, []byte("test"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err = blob.Upload(ctx, srcFile, "../../../etc/passwd")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "dangerous pattern") {
		t.Errorf("error %q does not contain %q", err.Error(), "dangerous pattern")
	}
}

func TestLocalFileBlobDownloadInvalidKey(t *testing.T) {
	ctx := t.Context()

	tmpDir := t.TempDir()
	rootDir := filepath.Join(tmpDir, "cache-root")
	destDir := filepath.Join(tmpDir, "dest")

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	blob, err := NewLocalFileBlob(ctx, fileURL(rootDir))
	if err != nil {
		t.Fatalf("NewLocalFileBlob: %v", err)
	}

	destFile := filepath.Join(destDir, "test.txt")
	_, err = blob.Download(ctx, "cache//invalid", destFile)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "dangerous pattern") {
		t.Errorf("error %q does not contain %q", err.Error(), "dangerous pattern")
	}
}

func TestKeyToPaths(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := t.Context()

	blob, err := NewLocalFileBlob(ctx, fileURL(tmpDir))
	if err != nil {
		t.Fatalf("NewLocalFileBlob: %v", err)
	}

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
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.errContains)
				}
			} else {
				if err != nil {
					t.Fatalf("keyToPaths: %v", err)
				}
				if dataPath != tt.wantData {
					t.Errorf("dataPath: got %q, want %q", dataPath, tt.wantData)
				}
				if metaPath != tt.wantMeta {
					t.Errorf("metaPath: got %q, want %q", metaPath, tt.wantMeta)
				}
			}
		})
	}
}

func TestLocalFileBlobConcurrentUpload(t *testing.T) {
	if runtime.GOOS == "windows" {
		// Windows can't atomically rename over an open or just-removed
		// target, so the "last-writer-wins" semantics that work on
		// POSIX systems surface as "Access is denied" here. The store
		// would need OS-specific locking to fix this.
		t.Skip("concurrent rename-to-same-key is not safe on Windows")
	}

	ctx := t.Context()

	tmpDir := t.TempDir()
	rootDir := filepath.Join(tmpDir, "cache-root")
	srcDir := filepath.Join(tmpDir, "source")

	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	blob, err := NewLocalFileBlob(ctx, fileURL(rootDir))
	if err != nil {
		t.Fatalf("NewLocalFileBlob: %v", err)
	}

	key := "test/cache/concurrent.txt"

	// Create two source files with different content
	srcFile1 := filepath.Join(srcDir, "file1.txt")
	content1 := []byte("Content from goroutine 1")
	if err := os.WriteFile(srcFile1, content1, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	srcFile2 := filepath.Join(srcDir, "file2.txt")
	content2 := []byte("Content from goroutine 2")
	if err := os.WriteFile(srcFile2, content2, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

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
	if err1 != nil {
		t.Fatalf("Upload goroutine 1: %v", err1)
	}
	if err2 != nil {
		t.Fatalf("Upload goroutine 2: %v", err2)
	}

	// Verify file exists and contains one of the two contents
	dataPath := filepath.Join(rootDir, "test/cache/concurrent.txt")
	if _, err := os.Stat(dataPath); err != nil {
		t.Errorf("expected file %q to exist: %v", dataPath, err)
	}

	finalContent, err := os.ReadFile(dataPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	// Should be one of the two contents (last writer wins)
	if !bytes.Equal(finalContent, content1) && !bytes.Equal(finalContent, content2) {
		t.Errorf("final content should be from one of the uploaders, got %q", finalContent)
	}
}

func TestNewBlobStoreLocalFile(t *testing.T) {
	ctx := t.Context()
	tmpDir := t.TempDir()

	blob, err := NewBlobStore(ctx, LocalFileStore, fileURL(tmpDir))
	if err != nil {
		t.Fatalf("NewBlobStore: %v", err)
	}
	if blob == nil {
		t.Fatal("expected non-nil blob")
	}

	if _, ok := blob.(*LocalFileBlob); !ok {
		t.Errorf("expected LocalFileBlob type, got %T", blob)
	}
}
