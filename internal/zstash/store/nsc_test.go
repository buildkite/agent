package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNscStore_Interface(t *testing.T) {
	// This test ensures that NscStore properly implements the Blob interface
	var _ Blob = (*NscStore)(nil)
}

func TestValidateFilePath(t *testing.T) {
	tests := []struct {
		name        string
		filePath    string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "valid simple path",
			filePath:    "test.txt",
			expectError: false,
		},
		{
			name:        "valid relative path",
			filePath:    "dir/subdir/file.txt",
			expectError: false,
		},
		{
			name:        "valid absolute path",
			filePath:    "/tmp/test.txt",
			expectError: false,
		},
		{
			name:        "empty path",
			filePath:    "",
			expectError: true,
			errorMsg:    "file path cannot be empty",
		},
		{
			name:        "path with semicolon",
			filePath:    "file;rm -rf /",
			expectError: true,
			errorMsg:    "file path contains potentially dangerous character: ;",
		},
		{
			name:        "path with ampersand",
			filePath:    "file&malicious",
			expectError: true,
			errorMsg:    "file path contains potentially dangerous character: &",
		},
		{
			name:        "path with pipe",
			filePath:    "file|cat /etc/passwd",
			expectError: true,
			errorMsg:    "file path contains potentially dangerous character: |",
		},
		{
			name:        "path with backtick",
			filePath:    "file`whoami`",
			expectError: true,
			errorMsg:    "file path contains potentially dangerous character: `",
		},
		{
			name:        "path with dollar sign",
			filePath:    "file$(whoami)",
			expectError: true,
			errorMsg:    "file path contains potentially dangerous character: $",
		},
		{
			name:        "path traversal attempt",
			filePath:    "../../../etc/passwd",
			expectError: true,
			errorMsg:    "file path contains path traversal sequence",
		},
		{
			name:        "path with quotes",
			filePath:    `file"test"`,
			expectError: true,
			errorMsg:    `file path contains potentially dangerous character: "`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateFilePath(tt.filePath)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateKey(t *testing.T) {
	tests := []struct {
		name        string
		key         string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "valid simple key",
			key:         "mykey",
			expectError: false,
		},
		{
			name:        "valid key with path",
			key:         "builds/123/artifacts/report.txt",
			expectError: false,
		},
		{
			name:        "valid key with dots and underscores",
			key:         "test_file.tar.gz",
			expectError: false,
		},
		{
			name:        "valid key with hyphens",
			key:         "build-artifact-v1.0.0",
			expectError: false,
		},
		{
			name:        "empty key",
			key:         "",
			expectError: true,
			errorMsg:    "key cannot be empty",
		},
		{
			name:        "key too long",
			key:         string(make([]byte, 257)), // 257 characters
			expectError: true,
			errorMsg:    "key too long (max 256 characters)",
		},
		{
			name:        "key with invalid characters",
			key:         "key with spaces",
			expectError: true,
			errorMsg:    "key contains invalid characters",
		},
		{
			name:        "key with special characters",
			key:         "key@domain.com",
			expectError: true,
			errorMsg:    "key contains invalid characters",
		},
		{
			name:        "key with path traversal",
			key:         "../secret",
			expectError: true,
			errorMsg:    "key contains potentially dangerous pattern: ../",
		},
		{
			name:        "key with command injection attempt",
			key:         "file&&rm-rf",
			expectError: true,
			errorMsg:    "key contains invalid characters", // regex catches it first
		},
		{
			name:        "key with backtick",
			key:         "file`whoami`",
			expectError: true,
			errorMsg:    "key contains invalid characters", // regex catches it first
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateKey(tt.key)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestRunCommandValidation(t *testing.T) {
	ctx := context.Background()

	// Test empty args
	result, err := runCommand(ctx, "" /* no args */)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no command provided")
	assert.Nil(t, result)
}

// TestNscStore_MockUpload tests the Upload method with mocked command execution
// Note: This test will fail if nsc is not installed, but shows the structure
func TestNscStore_Upload_Validation(t *testing.T) {
	store, err := NewNscStore()
	require.NoError(t, err)

	ctx := context.Background()

	// Create a temporary test file
	tmpDir, err := os.MkdirTemp("", "nsc-test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	testFile := filepath.Join(tmpDir, "test.txt")
	err = os.WriteFile(testFile, []byte("test content"), 0o600)
	require.NoError(t, err)

	// Test invalid file path
	_, err = store.Upload(ctx, "invalid;path", "valid-key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid file path")

	// Test invalid key
	_, err = store.Upload(ctx, testFile, "invalid key with spaces")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid key")

	// Test valid inputs (will fail with nsc command error, but validation passes)
	_, err = store.Upload(ctx, testFile, "valid-key")
	// This will error because nsc command likely doesn't exist or isn't configured
	// but the error should be about command execution, not validation
	if err != nil {
		assert.NotContains(t, err.Error(), "invalid file path")
		assert.NotContains(t, err.Error(), "invalid key")
	}
}

// TestNscStore_Download_Validation tests the Download method validation
func TestNscStore_Download_Validation(t *testing.T) {
	store, err := NewNscStore()
	require.NoError(t, err)

	ctx := context.Background()

	tmpDir, err := os.MkdirTemp("", "nsc-test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	destFile := filepath.Join(tmpDir, "download.txt")

	// Test invalid key
	_, err = store.Download(ctx, "invalid key with spaces", destFile)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid key")

	// Test invalid file path
	_, err = store.Download(ctx, "valid-key", "invalid;path")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid file path")

	// Test valid inputs (will fail with nsc command error, but validation passes)
	_, err = store.Download(ctx, "valid-key", destFile)
	// This will error because nsc command likely doesn't exist or isn't configured
	// but the error should be about command execution, not validation
	if err != nil {
		assert.NotContains(t, err.Error(), "invalid key")
		assert.NotContains(t, err.Error(), "invalid file path")
	}
}

// TestNscStore_Integration runs integration tests if NSC CLI is available
// This test can be skipped if NSC is not installed or configured
func TestNscStore_Integration(t *testing.T) {
	// Skip this test if NSC_INTEGRATION_TEST environment variable is not set
	if os.Getenv("NSC_INTEGRATION_TEST") == "" {
		t.Skip("Skipping NSC integration test (set NSC_INTEGRATION_TEST=1 to run)")
	}

	store, err := NewNscStore()
	require.NoError(t, err)

	ctx := context.Background()

	// Create temporary directories and files
	tmpDir, err := os.MkdirTemp("", "nsc-integration-test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create a test file
	testFile := filepath.Join(tmpDir, "test-upload.txt")
	testContent := "Hello from NSC integration test!"
	err = os.WriteFile(testFile, []byte(testContent), 0o600)
	require.NoError(t, err)

	// Test upload
	key := "integration-test/test-file.txt"
	transferInfo, err := store.Upload(ctx, testFile, key)
	require.NoError(t, err, "Upload should succeed with valid NSC setup")

	assert.Greater(t, transferInfo.BytesTransferred, int64(0))
	assert.Greater(t, transferInfo.TransferSpeed, 0.0)
	assert.Greater(t, transferInfo.Duration, 0)

	// Test download
	downloadFile := filepath.Join(tmpDir, "test-download.txt")
	transferInfo, err = store.Download(ctx, key, downloadFile)
	require.NoError(t, err, "Download should succeed")

	assert.Greater(t, transferInfo.BytesTransferred, int64(0))

	// Verify downloaded content
	downloadedContent, err := os.ReadFile(downloadFile)
	require.NoError(t, err)
	assert.Equal(t, testContent, string(downloadedContent))

	t.Logf("Upload: %d bytes at %.2f MB/s in %v",
		transferInfo.BytesTransferred,
		transferInfo.TransferSpeed,
		transferInfo.Duration)
}
