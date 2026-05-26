package store

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.errorMsg)
				}
			} else {
				if err != nil {
					t.Fatalf("validateFilePath: %v", err)
				}
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
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.errorMsg)
				}
			} else {
				if err != nil {
					t.Fatalf("validateKey: %v", err)
				}
			}
		})
	}
}

func TestRunCommandValidation(t *testing.T) {
	ctx := context.Background()

	// Test empty args
	result, err := runCommand(ctx, "" /* no args */)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "no command provided") {
		t.Errorf("error %q does not contain %q", err.Error(), "no command provided")
	}
	if result != nil {
		t.Errorf("expected nil result, got %v", result)
	}
}

// TestNscStore_MockUpload tests the Upload method with mocked command execution
// Note: This test will fail if nsc is not installed, but shows the structure
func TestNscStore_Upload_Validation(t *testing.T) {
	store, err := NewNscStore()
	if err != nil {
		t.Fatalf("NewNscStore: %v", err)
	}

	ctx := context.Background()

	// Create a temporary test file
	tmpDir, err := os.MkdirTemp("", "nsc-test")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	testFile := filepath.Join(tmpDir, "test.txt")
	err = os.WriteFile(testFile, []byte("test content"), 0o600)
	if err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Test invalid file path
	_, err = store.Upload(ctx, "invalid;path", "valid-key")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid file path") {
		t.Errorf("error %q does not contain %q", err.Error(), "invalid file path")
	}

	// Test invalid key
	_, err = store.Upload(ctx, testFile, "invalid key with spaces")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid key") {
		t.Errorf("error %q does not contain %q", err.Error(), "invalid key")
	}

	// Test valid inputs (will fail with nsc command error, but validation passes)
	_, err = store.Upload(ctx, testFile, "valid-key")
	// This will error because nsc command likely doesn't exist or isn't configured
	// but the error should be about command execution, not validation
	if err != nil {
		if strings.Contains(err.Error(), "invalid file path") {
			t.Errorf("error %q should not contain %q", err.Error(), "invalid file path")
		}
		if strings.Contains(err.Error(), "invalid key") {
			t.Errorf("error %q should not contain %q", err.Error(), "invalid key")
		}
	}
}

// TestNscStore_Download_Validation tests the Download method validation
func TestNscStore_Download_Validation(t *testing.T) {
	store, err := NewNscStore()
	if err != nil {
		t.Fatalf("NewNscStore: %v", err)
	}

	ctx := context.Background()

	tmpDir, err := os.MkdirTemp("", "nsc-test")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	destFile := filepath.Join(tmpDir, "download.txt")

	// Test invalid key
	_, err = store.Download(ctx, "invalid key with spaces", destFile)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid key") {
		t.Errorf("error %q does not contain %q", err.Error(), "invalid key")
	}

	// Test invalid file path
	_, err = store.Download(ctx, "valid-key", "invalid;path")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid file path") {
		t.Errorf("error %q does not contain %q", err.Error(), "invalid file path")
	}

	// Test valid inputs (will fail with nsc command error, but validation passes)
	_, err = store.Download(ctx, "valid-key", destFile)
	// This will error because nsc command likely doesn't exist or isn't configured
	// but the error should be about command execution, not validation
	if err != nil {
		if strings.Contains(err.Error(), "invalid key") {
			t.Errorf("error %q should not contain %q", err.Error(), "invalid key")
		}
		if strings.Contains(err.Error(), "invalid file path") {
			t.Errorf("error %q should not contain %q", err.Error(), "invalid file path")
		}
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
	if err != nil {
		t.Fatalf("NewNscStore: %v", err)
	}

	ctx := context.Background()

	// Create temporary directories and files
	tmpDir, err := os.MkdirTemp("", "nsc-integration-test")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create a test file
	testFile := filepath.Join(tmpDir, "test-upload.txt")
	testContent := "Hello from NSC integration test!"
	err = os.WriteFile(testFile, []byte(testContent), 0o600)
	if err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Test upload
	key := "integration-test/test-file.txt"
	transferInfo, err := store.Upload(ctx, testFile, key)
	if err != nil {
		t.Fatalf("Upload should succeed with valid NSC setup: %v", err)
	}

	if transferInfo.BytesTransferred <= 0 {
		t.Errorf("expected BytesTransferred > 0, got %d", transferInfo.BytesTransferred)
	}
	if transferInfo.TransferSpeed <= 0.0 {
		t.Errorf("expected TransferSpeed > 0, got %f", transferInfo.TransferSpeed)
	}
	if transferInfo.Duration <= 0 {
		t.Errorf("expected Duration > 0, got %v", transferInfo.Duration)
	}

	// Test download
	downloadFile := filepath.Join(tmpDir, "test-download.txt")
	transferInfo, err = store.Download(ctx, key, downloadFile)
	if err != nil {
		t.Fatalf("Download should succeed: %v", err)
	}

	if transferInfo.BytesTransferred <= 0 {
		t.Errorf("expected BytesTransferred > 0, got %d", transferInfo.BytesTransferred)
	}

	// Verify downloaded content
	downloadedContent, err := os.ReadFile(downloadFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(downloadedContent) != testContent {
		t.Errorf("downloaded content mismatch: got %q, want %q", string(downloadedContent), testContent)
	}

	t.Logf("Upload: %d bytes at %.2f MB/s in %v",
		transferInfo.BytesTransferred,
		transferInfo.TransferSpeed,
		transferInfo.Duration)
}
